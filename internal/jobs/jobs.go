package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/simpul/hr-backend/internal/domain"
	"github.com/simpul/hr-backend/internal/modules/payroll"
	"github.com/simpul/hr-backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TypePayrollProcess        = "payroll:process"
	TypeAttendanceDailyRollup = "attendance:daily-rollup"
	TypeLeaveAccrual          = "leave:balance-accrual"
	TypeTHRSchedule           = "payroll:thr-schedule"
	TypeNotificationDigest    = "notification:digest"
)

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func AsynqRedis(config RedisConfig) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: config.Addr, Password: config.Password, DB: config.DB}
}

type Dispatcher struct {
	db     *gorm.DB
	client *asynq.Client
	logger *slog.Logger
}

func NewDispatcher(db *gorm.DB, client *asynq.Client, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{db: db, client: client, logger: logger}
}

func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := d.dispatchBatch(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.logger.Error("dispatch outbox", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) dispatchBatch(ctx context.Context) error {
	var events []domain.OutboxEvent
	if err := d.db.WithContext(ctx).
		Where("published_at IS NULL AND available_at <= ?", time.Now().UTC()).
		Order("created_at").Limit(25).Find(&events).Error; err != nil {
		return err
	}
	for _, event := range events {
		task := asynq.NewTask(event.Topic, event.Payload)
		info, err := d.client.EnqueueContext(ctx, task, asynq.TaskID(event.ID), asynq.MaxRetry(10))
		if err != nil {
			d.db.Model(&domain.OutboxEvent{}).Where("id = ?", event.ID).
				Updates(map[string]any{"attempts": gorm.Expr("attempts + 1"), "last_error": err.Error(), "available_at": time.Now().UTC().Add(10 * time.Second)})
			continue
		}
		now := time.Now().UTC()
		if err := d.db.Model(&domain.OutboxEvent{}).Where("id = ? AND published_at IS NULL", event.ID).
			Updates(map[string]any{"published_at": now, "last_error": ""}).Error; err != nil {
			return err
		}
		d.logger.Info("outbox dispatched", "eventId", event.ID, "taskId", info.ID, "topic", event.Topic)
	}
	return nil
}

type Worker struct {
	db     *gorm.DB
	redis  *redis.Client
	logger *slog.Logger
}

func NewWorker(db *gorm.DB, redisClient *redis.Client, logger *slog.Logger) *Worker {
	return &Worker{db: db, redis: redisClient, logger: logger}
}

func (w *Worker) Mux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypePayrollProcess, w.processPayroll)
	mux.HandleFunc(TypeAttendanceDailyRollup, w.attendanceRollup)
	mux.HandleFunc(TypeLeaveAccrual, w.leaveAccrual)
	mux.HandleFunc(TypeTHRSchedule, w.thrSchedule)
	mux.HandleFunc(TypeNotificationDigest, w.notificationDigest)
	return mux
}

func ScheduleOrganizationTask(ctx context.Context, db *gorm.DB, topic string, logger *slog.Logger) {
	var organizationIDs []string
	if err := db.WithContext(ctx).Model(&domain.Organization{}).Pluck("id", &organizationIDs).Error; err != nil {
		logger.Error("load organizations for scheduled task", "topic", topic, "error", err)
		return
	}
	for _, organizationID := range organizationIDs {
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.organization_id', ?, true)", organizationID).Error; err != nil {
				return err
			}
			return repository.AddOutbox(tx, organizationID, topic, map[string]string{"organizationId": organizationID})
		})
		if err != nil {
			logger.Error("schedule organization task", "topic", topic, "organizationId", organizationID, "error", err)
		}
	}
}

func (w *Worker) processPayroll(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		OrganizationID string `json:"organizationId"`
		PayrollRunID   string `json:"payrollRunId"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payroll task: %w", err)
	}
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.organization_id', ?, true)", payload.OrganizationID).Error; err != nil {
			return err
		}
		var run domain.PayrollRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id = ? AND id = ?", payload.OrganizationID, payload.PayrollRunID).First(&run).Error; err != nil {
			return err
		}
		if run.Status == "completed" {
			return nil
		}
		if run.Status != "queued" && run.Status != "processing" {
			return asynq.SkipRetry
		}
		if err := tx.Model(&run).Update("status", "processing").Error; err != nil {
			return err
		}
		var policyModel domain.PayrollPolicy
		if err := tx.Where("organization_id = ? AND id = ?", payload.OrganizationID, run.PolicyID).First(&policyModel).Error; err != nil {
			return err
		}
		policyConfig, err := payroll.ParsePolicy(policyModel.Config)
		if err != nil {
			return err
		}
		var compensations []domain.Compensation
		if err := tx.Where("organization_id = ? AND effective_from <= ? AND (effective_to IS NULL OR effective_to >= ?)", payload.OrganizationID, run.PeriodEnd, run.PeriodStart).Find(&compensations).Error; err != nil {
			return err
		}
		var totalGross, totalDeductions, totalNet int64
		for _, compensation := range compensations {
			result := payroll.Calculate(payroll.Input{
				MonthlyGross: compensation.BaseSalary, TaxStatus: compensation.TaxStatus,
				BPJSHealthEnabled:     compensation.BPJSHealthEnabled,
				BPJSEmploymentEnabled: compensation.BPJSEmploymentEnabled,
			}, policyConfig)
			breakdown, _ := json.Marshal(result)
			payslip := domain.Payslip{
				OrganizationID: payload.OrganizationID, PayrollRunID: run.ID, EmployeeID: compensation.EmployeeID,
				GrossPay: result.GrossPay, BPJSDeduction: result.BPJSDeduction, PPh21Deduction: result.PPh21Deduction,
				OtherDeduction: result.OtherDeduction, NetPay: result.NetPay, Breakdown: breakdown,
			}
			if err := tx.Where("payroll_run_id = ? AND employee_id = ?", run.ID, compensation.EmployeeID).
				Assign(payslip).FirstOrCreate(&payslip).Error; err != nil {
				return err
			}
			totalGross += result.GrossPay
			totalDeductions += result.BPJSDeduction + result.PPh21Deduction + result.OtherDeduction
			totalNet += result.NetPay
		}
		now := time.Now().UTC()
		if err := tx.Model(&run).Updates(map[string]any{
			"status": "completed", "total_gross": totalGross, "total_deductions": totalDeductions,
			"total_net": totalNet, "processed_at": now, "failure_message": "",
		}).Error; err != nil {
			return err
		}
		return repository.AddOutbox(tx, payload.OrganizationID, TypeNotificationDigest, map[string]string{"organizationId": payload.OrganizationID, "payrollRunId": run.ID, "status": "completed"})
	})
	if err == nil || errors.Is(err, asynq.SkipRetry) {
		return err
	}
	failure := err.Error()
	if len(failure) > 1000 {
		failure = failure[:1000]
	}
	result := w.db.WithContext(ctx).Model(&domain.PayrollRun{}).
		Where("organization_id=? AND id=? AND status IN ('queued','processing')", payload.OrganizationID, payload.PayrollRunID).
		Updates(map[string]any{"status": "failed", "failure_message": failure})
	if result.Error != nil {
		return err
	}
	return fmt.Errorf("%w: payroll processing failed: %v", asynq.SkipRetry, err)
}

func (w *Worker) attendanceRollup(ctx context.Context, task *asynq.Task) error {
	organizationID, err := organizationFromTask(task)
	if err != nil {
		return err
	}
	var organization domain.Organization
	if err := w.db.WithContext(ctx).First(&organization, "id = ?", organizationID).Error; err != nil {
		return err
	}
	location, err := time.LoadLocation(organization.Timezone)
	if err != nil {
		location, _ = time.LoadLocation("Asia/Jakarta")
	}
	date := time.Now().In(location).AddDate(0, 0, -1).Format("2006-01-02")
	return w.db.WithContext(ctx).Exec(`
		UPDATE attendances SET status = 'anomaly', updated_at = now()
		WHERE organization_id = ? AND date = ? AND (clock_in IS NULL OR clock_out IS NULL) AND status <> 'on_leave'
	`, organizationID, date).Error
}

func (w *Worker) leaveAccrual(ctx context.Context, task *asynq.Task) error {
	organizationID, err := organizationFromTask(task)
	if err != nil {
		return err
	}
	return w.db.WithContext(ctx).Exec(`
		INSERT INTO leave_balances (organization_id, employee_id, leave_type_id, year, quota, taken)
		SELECT e.organization_id, e.id, lt.id, EXTRACT(YEAR FROM CURRENT_DATE)::int, lt.annual_quota, 0
		FROM employees e JOIN leave_types lt ON lt.organization_id = e.organization_id
		WHERE e.organization_id = ? AND e.status = 'active'
		ON CONFLICT (employee_id, leave_type_id, year) DO NOTHING
	`, organizationID).Error
}

func (w *Worker) notificationDigest(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		OrganizationID string `json:"organizationId"`
		LeaveRequestID string `json:"leaveRequestId"`
		ConversationID string `json:"conversationId"`
		MessageID      string `json:"messageId"`
		PayrollRunID   string `json:"payrollRunId"`
		Status         string `json:"status"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.OrganizationID == "" {
		return errors.New("notification task requires organizationId")
	}
	if payload.LeaveRequestID != "" && payload.Status == "" {
		eventKey := "leave-pending:" + payload.LeaveRequestID
		err := w.db.WithContext(ctx).Exec(`
			INSERT INTO notifications (organization_id,user_id,title,message,kind,payload)
			SELECT lr.organization_id,m.user_id,'Pengajuan Cuti Baru',e.full_name || ' mengajukan cuti untuk ' || lr.duration_days || ' hari.','urgent',jsonb_build_object('eventKey',?)
			FROM leave_requests lr JOIN employees e ON e.id=lr.employee_id
			JOIN memberships m ON m.organization_id=lr.organization_id
			JOIN roles r ON r.id=m.role_id
			WHERE lr.organization_id=? AND lr.id=? AND (r.key='hr_admin' OR m.employee_id=e.manager_id)
			  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.organization_id=lr.organization_id AND n.user_id=m.user_id AND n.payload->>'eventKey'=? )
		`, eventKey, payload.OrganizationID, payload.LeaveRequestID, eventKey).Error
		return w.publishNotification(ctx, payload.OrganizationID, err)
	}
	if payload.LeaveRequestID != "" {
		eventKey := "leave-status:" + payload.LeaveRequestID + ":" + payload.Status
		err := w.db.WithContext(ctx).Exec(`
			INSERT INTO notifications (organization_id,user_id,title,message,kind,payload)
			SELECT lr.organization_id,m.user_id,'Status Pengajuan Cuti','Pengajuan cuti Anda berstatus ' || ? || '.','info',jsonb_build_object('eventKey',?)
			FROM leave_requests lr JOIN memberships m ON m.employee_id=lr.employee_id AND m.organization_id=lr.organization_id
			WHERE lr.organization_id=? AND lr.id=?
			  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.organization_id=lr.organization_id AND n.user_id=m.user_id AND n.payload->>'eventKey'=? )
		`, payload.Status, eventKey, payload.OrganizationID, payload.LeaveRequestID, eventKey).Error
		return w.publishNotification(ctx, payload.OrganizationID, err)
	}
	if payload.MessageID != "" {
		eventKey := "message:" + payload.MessageID
		err := w.db.WithContext(ctx).Exec(`
			INSERT INTO notifications (organization_id,user_id,title,message,kind,payload)
			SELECT msg.organization_id,cm.user_id,'Pesan Baru',left(msg.body,180),'message',jsonb_build_object('eventKey',?,'conversationId',msg.conversation_id)
			FROM messages msg JOIN conversation_members cm ON cm.conversation_id=msg.conversation_id AND cm.user_id<>msg.sender_user_id
			WHERE msg.organization_id=? AND msg.id=?
			  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.organization_id=msg.organization_id AND n.user_id=cm.user_id AND n.payload->>'eventKey'=? )
		`, eventKey, payload.OrganizationID, payload.MessageID, eventKey).Error
		return w.publishNotification(ctx, payload.OrganizationID, err)
	}
	if payload.PayrollRunID != "" {
		eventKey := "payroll:" + payload.PayrollRunID + ":" + payload.Status
		err := w.db.WithContext(ctx).Exec(`
			INSERT INTO notifications (organization_id,user_id,title,message,kind,payload)
			SELECT m.organization_id,m.user_id,'Payroll Selesai','Payroll run telah selesai diproses.','info',jsonb_build_object('eventKey',?,'payrollRunId',?)
			FROM memberships m JOIN roles r ON r.id=m.role_id
			WHERE m.organization_id=? AND r.key IN ('hr_admin','finance_manager')
			  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.organization_id=m.organization_id AND n.user_id=m.user_id AND n.payload->>'eventKey'=? )
		`, eventKey, payload.PayrollRunID, payload.OrganizationID, eventKey).Error
		return w.publishNotification(ctx, payload.OrganizationID, err)
	}
	w.logger.Info("notification digest tick", "organizationId", payload.OrganizationID)
	return nil
}

func (w *Worker) publishNotification(ctx context.Context, organizationID string, databaseErr error) error {
	if databaseErr != nil {
		return databaseErr
	}
	if w.redis == nil {
		return nil
	}
	raw, _ := json.Marshal(map[string]string{"type": "notification.created"})
	if err := w.redis.Publish(ctx, "simpul:org:"+organizationID, raw).Err(); err != nil {
		w.logger.Warn("notification realtime publish failed", "organizationId", organizationID, "error", err)
	}
	return nil
}

func (w *Worker) thrSchedule(ctx context.Context, task *asynq.Task) error {
	organizationID, err := organizationFromTask(task)
	if err != nil {
		return err
	}
	var organization domain.Organization
	if err := w.db.WithContext(ctx).First(&organization, "id = ?", organizationID).Error; err != nil {
		return err
	}
	location, err := time.LoadLocation(organization.Timezone)
	if err != nil {
		location, _ = time.LoadLocation("Asia/Jakarta")
	}
	year := time.Now().In(location).Year()
	title := fmt.Sprintf("Verifikasi Jadwal THR %d", year)
	return w.db.WithContext(ctx).Exec(`
		INSERT INTO notifications (organization_id,user_id,title,message,kind)
		SELECT m.organization_id,m.user_id,?, 'Periksa policy efektif, tanggal hari raya, dan kesiapan run THR. Kalkulasi demo berstatus nonCertified.', 'urgent'
		FROM memberships m JOIN roles r ON r.id=m.role_id
		WHERE m.organization_id=? AND r.key='hr_admin'
		  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.organization_id=m.organization_id AND n.user_id=m.user_id AND n.title=?)
	`, title, organizationID, title).Error
}

func organizationFromTask(task *asynq.Task) (string, error) {
	var payload struct {
		OrganizationID string `json:"organizationId"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return "", fmt.Errorf("decode organization task: %w", err)
	}
	if payload.OrganizationID == "" {
		return "", errors.New("decode organization task: organizationId is required")
	}
	return payload.OrganizationID, nil
}
