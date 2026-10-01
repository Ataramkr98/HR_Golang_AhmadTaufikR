package domain

import (
	"encoding/json"
	"time"
)

type Organization struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Slug      string    `gorm:"uniqueIndex;not null" json:"slug"`
	Name      string    `gorm:"not null" json:"name"`
	Industry  string    `json:"industry"`
	Address   string    `json:"address"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	Timezone  string    `gorm:"not null;default:Asia/Jakarta" json:"timezone"`
	Locale    string    `gorm:"not null;default:id-ID" json:"locale"`
	Currency  string    `gorm:"not null;default:IDR" json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type User struct {
	ID           string       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Email        string       `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash string       `gorm:"not null" json:"-"`
	MFASecretEnc string       `json:"-"`
	MFAEnabled   bool         `json:"mfaEnabled"`
	LastLoginAt  *time.Time   `json:"lastLoginAt,omitempty"`
	Memberships  []Membership `json:"memberships,omitempty"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
}

type Membership struct {
	ID             string       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string       `gorm:"type:uuid;not null;uniqueIndex:idx_membership_user_org" json:"organizationId"`
	UserID         string       `gorm:"type:uuid;not null;uniqueIndex:idx_membership_user_org" json:"userId"`
	EmployeeID     *string      `gorm:"type:uuid" json:"employeeId,omitempty"`
	RoleID         string       `gorm:"type:uuid;not null" json:"roleId"`
	Organization   Organization `json:"organization,omitempty"`
	Role           Role         `json:"role,omitempty"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

type Role struct {
	ID             string       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string       `gorm:"type:uuid;not null;uniqueIndex:idx_role_org_key" json:"organizationId"`
	Key            string       `gorm:"not null;uniqueIndex:idx_role_org_key" json:"key"`
	Name           string       `gorm:"not null" json:"name"`
	Description    string       `json:"description"`
	IsSystem       bool         `json:"isSystem"`
	Permissions    []Permission `gorm:"many2many:role_permissions" json:"permissions,omitempty"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

type Permission struct {
	ID          string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Key         string `gorm:"uniqueIndex;not null" json:"key"`
	Label       string `gorm:"not null" json:"label"`
	Description string `json:"description"`
}

type RefreshSession struct {
	ID             string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID         string     `gorm:"type:uuid;not null;index" json:"userId"`
	MembershipID   string     `gorm:"type:uuid;not null" json:"membershipId"`
	OrganizationID string     `gorm:"type:uuid;not null;index" json:"organizationId"`
	TokenHash      string     `gorm:"uniqueIndex;not null" json:"-"`
	ExpiresAt      time.Time  `gorm:"not null" json:"expiresAt"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
	UserAgent      string     `json:"userAgent"`
	IPAddress      string     `json:"ipAddress"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type MFARecoveryCode struct {
	ID        string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    string `gorm:"type:uuid;not null;index"`
	CodeHash  string `gorm:"not null"`
	UsedAt    *time.Time
	CreatedAt time.Time
}

type Department struct {
	ID                 string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID     string    `gorm:"type:uuid;not null;uniqueIndex:idx_department_org_name" json:"organizationId"`
	Name               string    `gorm:"not null;uniqueIndex:idx_department_org_name" json:"name"`
	ParentDepartmentID *string   `gorm:"type:uuid" json:"parentDepartmentId,omitempty"`
	HeadEmployeeID     *string   `gorm:"type:uuid" json:"headEmployeeId,omitempty"`
	EmployeeCount      int64     `gorm:"-" json:"employeeCount"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type Position struct {
	ID             string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string     `gorm:"type:uuid;not null;index" json:"organizationId"`
	DepartmentID   string     `gorm:"type:uuid;not null;index" json:"departmentId"`
	Title          string     `gorm:"not null" json:"title"`
	Level          string     `json:"level"`
	Department     Department `json:"department,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type Employee struct {
	ID             string        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string        `gorm:"type:uuid;not null;uniqueIndex:idx_employee_org_code" json:"organizationId"`
	EmployeeCode   string        `gorm:"not null;uniqueIndex:idx_employee_org_code" json:"employeeCode"`
	FullName       string        `gorm:"not null;index" json:"fullName"`
	Email          string        `gorm:"not null" json:"email"`
	Phone          string        `json:"phone"`
	HireDate       time.Time     `gorm:"type:date;not null" json:"hireDate"`
	EndDate        *time.Time    `gorm:"type:date" json:"endDate,omitempty"`
	Status         string        `gorm:"not null" json:"status"`
	DepartmentID   string        `gorm:"type:uuid;not null;index" json:"departmentId"`
	PositionID     string        `gorm:"type:uuid;not null;index" json:"positionId"`
	ManagerID      *string       `gorm:"type:uuid;index" json:"managerId,omitempty"`
	Address        string        `json:"address"`
	Education      string        `json:"education"`
	BirthDate      *time.Time    `gorm:"type:date" json:"birthDate,omitempty"`
	Gender         string        `json:"gender,omitempty"`
	EmergencyName  string        `json:"emergencyName,omitempty"`
	EmergencyPhone string        `json:"emergencyPhone,omitempty"`
	NIKEncrypted   string        `json:"-"`
	Department     Department    `json:"department,omitempty"`
	Position       Position      `json:"position,omitempty"`
	Manager        *Employee     `gorm:"foreignKey:ManagerID" json:"manager,omitempty"`
	Compensation   *Compensation `json:"compensation,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
	DeletedAt      *time.Time    `gorm:"index" json:"-"`
}

type Compensation struct {
	ID                    string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID        string     `gorm:"type:uuid;not null;index" json:"organizationId"`
	EmployeeID            string     `gorm:"type:uuid;not null;uniqueIndex" json:"employeeId"`
	BaseSalary            int64      `gorm:"not null" json:"baseSalary"`
	TaxStatus             string     `json:"taxStatus"`
	BPJSHealthEnabled     bool       `json:"bpjsHealthEnabled"`
	BPJSEmploymentEnabled bool       `json:"bpjsEmploymentEnabled"`
	BankName              string     `json:"bankName,omitempty"`
	BankAccountEnc        string     `json:"-"`
	EffectiveFrom         time.Time  `gorm:"type:date;not null" json:"effectiveFrom"`
	EffectiveTo           *time.Time `gorm:"type:date" json:"effectiveTo,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type Holiday struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;uniqueIndex:idx_holiday_org_date" json:"organizationId"`
	Date           time.Time `gorm:"type:date;not null;uniqueIndex:idx_holiday_org_date" json:"date"`
	Name           string    `gorm:"not null" json:"name"`
}

type Attendance struct {
	ID              string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID  string     `gorm:"type:uuid;not null;uniqueIndex:idx_attendance_emp_date" json:"organizationId"`
	EmployeeID      string     `gorm:"type:uuid;not null;uniqueIndex:idx_attendance_emp_date" json:"employeeId"`
	Date            time.Time  `gorm:"type:date;not null;uniqueIndex:idx_attendance_emp_date" json:"date"`
	ClockIn         *time.Time `json:"clockIn,omitempty"`
	ClockOut        *time.Time `json:"clockOut,omitempty"`
	TotalMinutes    int        `json:"totalMinutes"`
	OvertimeMinutes int        `json:"overtimeMinutes"`
	Status          string     `gorm:"not null" json:"status"`
	Source          string     `gorm:"not null" json:"source"`
	Notes           string     `json:"notes,omitempty"`
	Employee        Employee   `json:"employee,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type ShiftAssignment struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;uniqueIndex:idx_shift_emp_date" json:"organizationId"`
	EmployeeID     string    `gorm:"type:uuid;not null;uniqueIndex:idx_shift_emp_date" json:"employeeId"`
	Date           time.Time `gorm:"type:date;not null;uniqueIndex:idx_shift_emp_date" json:"date"`
	ShiftType      string    `gorm:"not null" json:"shiftType"`
	StartTime      string    `json:"startTime"`
	EndTime        string    `json:"endTime"`
	Employee       Employee  `json:"employee,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type LeaveType struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;uniqueIndex:idx_leave_type_org_name" json:"organizationId"`
	Name           string    `gorm:"not null;uniqueIndex:idx_leave_type_org_name" json:"name"`
	AnnualQuota    int       `gorm:"not null" json:"annualQuota"`
	Color          string    `json:"color"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type LeaveBalance struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;uniqueIndex:idx_leave_balance" json:"organizationId"`
	EmployeeID     string    `gorm:"type:uuid;not null;uniqueIndex:idx_leave_balance" json:"employeeId"`
	LeaveTypeID    string    `gorm:"type:uuid;not null;uniqueIndex:idx_leave_balance" json:"leaveTypeId"`
	Year           int       `gorm:"not null;uniqueIndex:idx_leave_balance" json:"year"`
	Quota          int       `gorm:"not null" json:"quota"`
	Taken          int       `gorm:"not null" json:"taken"`
	LeaveType      LeaveType `json:"leaveType,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type LeaveRequest struct {
	ID             string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string     `gorm:"type:uuid;not null;index" json:"organizationId"`
	EmployeeID     string     `gorm:"type:uuid;not null;index" json:"employeeId"`
	LeaveTypeID    string     `gorm:"type:uuid;not null" json:"leaveTypeId"`
	StartDate      time.Time  `gorm:"type:date;not null" json:"startDate"`
	EndDate        time.Time  `gorm:"type:date;not null" json:"endDate"`
	DurationDays   int        `gorm:"not null" json:"durationDays"`
	Reason         string     `gorm:"not null" json:"reason"`
	Status         string     `gorm:"not null;index" json:"status"`
	ApproverID     *string    `gorm:"type:uuid" json:"approverId,omitempty"`
	DecisionNote   string     `json:"decisionNote,omitempty"`
	DecidedAt      *time.Time `json:"decidedAt,omitempty"`
	Employee       Employee   `json:"employee,omitempty"`
	LeaveType      LeaveType  `json:"leaveType,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type PayrollPolicy struct {
	ID             string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string          `gorm:"type:uuid;not null;index" json:"organizationId"`
	Name           string          `gorm:"not null" json:"name"`
	EffectiveFrom  time.Time       `gorm:"type:date;not null" json:"effectiveFrom"`
	EffectiveTo    *time.Time      `gorm:"type:date" json:"effectiveTo,omitempty"`
	Config         json.RawMessage `gorm:"type:jsonb;not null" json:"config"`
	Certified      bool            `json:"certified"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type PayrollRun struct {
	ID              string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID  string     `gorm:"type:uuid;not null;index" json:"organizationId"`
	PolicyID        string     `gorm:"type:uuid;not null" json:"policyId"`
	PeriodStart     time.Time  `gorm:"type:date;not null" json:"periodStart"`
	PeriodEnd       time.Time  `gorm:"type:date;not null" json:"periodEnd"`
	Status          string     `gorm:"not null;index" json:"status"`
	TotalGross      int64      `json:"totalGross"`
	TotalDeductions int64      `json:"totalDeductions"`
	TotalNet        int64      `json:"totalNet"`
	FailureMessage  string     `json:"failureMessage,omitempty"`
	ProcessedAt     *time.Time `json:"processedAt,omitempty"`
	Payslips        []Payslip  `json:"payslips,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type Payslip struct {
	ID             string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string          `gorm:"type:uuid;not null;index" json:"organizationId"`
	PayrollRunID   string          `gorm:"type:uuid;not null;uniqueIndex:idx_payslip_run_employee" json:"payrollRunId"`
	EmployeeID     string          `gorm:"type:uuid;not null;uniqueIndex:idx_payslip_run_employee" json:"employeeId"`
	GrossPay       int64           `json:"grossPay"`
	BPJSDeduction  int64           `json:"bpjsDeduction"`
	PPh21Deduction int64           `json:"pph21Deduction"`
	OtherDeduction int64           `json:"otherDeduction"`
	NetPay         int64           `json:"netPay"`
	Breakdown      json.RawMessage `gorm:"type:jsonb" json:"breakdown"`
	Employee       Employee        `json:"employee,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type JobPosting struct {
	ID             string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string     `gorm:"type:uuid;not null;index" json:"organizationId"`
	DepartmentID   string     `gorm:"type:uuid;not null" json:"departmentId"`
	Title          string     `gorm:"not null" json:"title"`
	Status         string     `gorm:"not null" json:"status"`
	Description    string     `json:"description"`
	Department     Department `json:"department,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type Candidate struct {
	ID             string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string     `gorm:"type:uuid;not null;index" json:"organizationId"`
	JobPostingID   string     `gorm:"type:uuid;not null" json:"jobPostingId"`
	FullName       string     `gorm:"not null" json:"fullName"`
	Email          string     `gorm:"not null" json:"email"`
	Source         string     `json:"source"`
	Rating         int        `json:"rating"`
	Stage          string     `gorm:"not null;index" json:"stage"`
	JobPosting     JobPosting `json:"jobPosting,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type CandidateStageHistory struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;index" json:"organizationId"`
	CandidateID    string    `gorm:"type:uuid;not null;index" json:"candidateId"`
	FromStage      string    `json:"fromStage"`
	ToStage        string    `gorm:"not null" json:"toStage"`
	ActorID        string    `gorm:"type:uuid;not null" json:"actorId"`
	CreatedAt      time.Time `json:"createdAt"`
}

type PerformanceCycle struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;index" json:"organizationId"`
	Name           string    `gorm:"not null" json:"name"`
	StartDate      time.Time `gorm:"type:date;not null" json:"startDate"`
	EndDate        time.Time `gorm:"type:date;not null" json:"endDate"`
	Status         string    `gorm:"not null" json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Goal struct {
	ID             string           `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string           `gorm:"type:uuid;not null;index" json:"organizationId"`
	EmployeeID     string           `gorm:"type:uuid;not null;index" json:"employeeId"`
	CycleID        string           `gorm:"type:uuid;not null;index" json:"cycleId"`
	Title          string           `gorm:"not null" json:"title"`
	Category       string           `json:"category"`
	Progress       int              `json:"progress"`
	Weight         int              `json:"weight"`
	Employee       Employee         `json:"employee,omitempty"`
	Cycle          PerformanceCycle `json:"cycle,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

type Review struct {
	ID             string           `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string           `gorm:"type:uuid;not null;index" json:"organizationId"`
	CycleID        string           `gorm:"type:uuid;not null" json:"cycleId"`
	EmployeeID     string           `gorm:"type:uuid;not null" json:"employeeId"`
	ReviewerID     string           `gorm:"type:uuid;not null" json:"reviewerId"`
	Relation       string           `json:"relation"`
	Rating         int              `json:"rating"`
	Notes          string           `json:"notes"`
	Reviewer       Employee         `gorm:"foreignKey:ReviewerID" json:"reviewer,omitempty"`
	Cycle          PerformanceCycle `json:"cycle,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

type Integration struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;uniqueIndex:idx_integration_org_provider" json:"organizationId"`
	Provider       string    `gorm:"not null;uniqueIndex:idx_integration_org_provider" json:"provider"`
	Enabled        bool      `json:"enabled"`
	ConfigEnc      string    `json:"-"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Notification struct {
	ID             string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string          `gorm:"type:uuid;not null;index" json:"organizationId"`
	UserID         string          `gorm:"type:uuid;not null;index" json:"userId"`
	Title          string          `gorm:"not null" json:"title"`
	Message        string          `gorm:"not null" json:"message"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `gorm:"type:jsonb" json:"payload,omitempty"`
	ReadAt         *time.Time      `json:"readAt,omitempty"`
	DismissedAt    *time.Time      `json:"dismissedAt,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type Conversation struct {
	ID             string               `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string               `gorm:"type:uuid;not null;index" json:"organizationId"`
	Title          string               `json:"title"`
	Members        []ConversationMember `json:"members,omitempty"`
	Messages       []Message            `json:"messages,omitempty"`
	CreatedAt      time.Time            `json:"createdAt"`
	UpdatedAt      time.Time            `json:"updatedAt"`
}

type ConversationMember struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;index" json:"organizationId"`
	ConversationID string    `gorm:"type:uuid;not null;uniqueIndex:idx_conversation_member" json:"conversationId"`
	UserID         string    `gorm:"type:uuid;not null;uniqueIndex:idx_conversation_member" json:"userId"`
	User           User      `json:"user,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Message struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;index" json:"organizationId"`
	ConversationID string    `gorm:"type:uuid;not null;index" json:"conversationId"`
	SenderUserID   string    `gorm:"type:uuid;not null" json:"senderUserId"`
	Body           string    `gorm:"not null" json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
}

type FAQ struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;index" json:"organizationId"`
	Category       string    `gorm:"not null" json:"category"`
	Question       string    `gorm:"not null" json:"question"`
	Answer         string    `gorm:"not null" json:"answer"`
	SortOrder      int       `json:"sortOrder"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type SupportTicket struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string    `gorm:"type:uuid;not null;index" json:"organizationId"`
	RequesterID    string    `gorm:"type:uuid;not null" json:"requesterId"`
	Subject        string    `gorm:"not null" json:"subject"`
	Category       string    `gorm:"not null" json:"category"`
	Message        string    `gorm:"not null" json:"message"`
	Status         string    `gorm:"not null" json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type AttentionDismissal struct {
	ID             string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	OrganizationID string `gorm:"type:uuid;not null;index"`
	UserID         string `gorm:"type:uuid;not null;uniqueIndex:idx_attention_dismissal"`
	AttentionKey   string `gorm:"not null;uniqueIndex:idx_attention_dismissal"`
	DismissedAt    time.Time
}

type AuditLog struct {
	ID             string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string          `gorm:"type:uuid;not null;index" json:"organizationId"`
	ActorUserID    *string         `gorm:"type:uuid" json:"actorUserId,omitempty"`
	RequestID      string          `json:"requestId"`
	Action         string          `gorm:"not null" json:"action"`
	EntityType     string          `gorm:"not null" json:"entityType"`
	EntityID       string          `gorm:"type:uuid;not null" json:"entityId"`
	Before         json.RawMessage `gorm:"type:jsonb" json:"before,omitempty"`
	After          json.RawMessage `gorm:"type:jsonb" json:"after,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type OutboxEvent struct {
	ID             string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrganizationID string          `gorm:"type:uuid;not null;index" json:"organizationId"`
	Topic          string          `gorm:"not null;index" json:"topic"`
	Payload        json.RawMessage `gorm:"type:jsonb;not null" json:"payload"`
	AvailableAt    time.Time       `gorm:"not null;index" json:"availableAt"`
	PublishedAt    *time.Time      `json:"publishedAt,omitempty"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"lastError,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type IdempotencyKey struct {
	ID             string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	OrganizationID string          `gorm:"type:uuid;not null;uniqueIndex:idx_idempotency_scope"`
	Scope          string          `gorm:"not null;uniqueIndex:idx_idempotency_scope"`
	Key            string          `gorm:"not null;uniqueIndex:idx_idempotency_scope"`
	ResourceID     string          `gorm:"type:uuid"`
	Response       json.RawMessage `gorm:"type:jsonb"`
	ExpiresAt      time.Time       `gorm:"not null"`
	CreatedAt      time.Time
}
