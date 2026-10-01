package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simpul/hr-backend/internal/domain"
	"github.com/simpul/hr-backend/internal/platform/secure"
	"github.com/simpul/hr-backend/internal/repository"
	"gorm.io/gorm"
)

func (s *Server) registerCollaborationRoutes(r *gin.RouterGroup) {
	r.GET("/notifications", s.listNotifications)
	r.POST("/notifications/:id/read", s.readNotification)
	r.DELETE("/notifications/:id", s.dismissNotification)
	r.GET("/conversations", s.listConversations)
	r.POST("/conversations", s.createConversation)
	r.GET("/conversations/:id/messages", s.listMessages)
	r.POST("/conversations/:id/messages", s.createMessage)
	r.POST("/realtime/tickets", s.realtimeTicket)
	r.GET("/faqs", s.listFAQs)
	r.POST("/support-tickets", s.createSupportTicket)
	r.GET("/support-tickets", s.listSupportTickets)
}

func (s *Server) listNotifications(c *gin.Context) {
	id := identityFrom(c)
	page, size := pagination(c)
	var items []domain.Notification
	var total int64
	q := s.db.Model(&domain.Notification{}).Where("organization_id=? AND user_id=? AND dismissed_at IS NULL", id.OrganizationID, id.UserID)
	q.Count(&total)
	if err := q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}})
}
func (s *Server) readNotification(c *gin.Context) {
	id := identityFrom(c)
	now := time.Now().UTC()
	result := s.db.Model(&domain.Notification{}).Where("organization_id=? AND user_id=? AND id=?", id.OrganizationID, id.UserID, c.Param("id")).Update("read_at", now)
	if result.Error != nil {
		databaseProblem(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		databaseProblem(c, gorm.ErrRecordNotFound)
		return
	}
	c.Status(http.StatusNoContent)
}
func (s *Server) dismissNotification(c *gin.Context) {
	id := identityFrom(c)
	now := time.Now().UTC()
	result := s.db.Model(&domain.Notification{}).Where("organization_id=? AND user_id=? AND id=?", id.OrganizationID, id.UserID, c.Param("id")).Update("dismissed_at", now)
	if result.Error != nil {
		databaseProblem(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		databaseProblem(c, gorm.ErrRecordNotFound)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listConversations(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Conversation
	if err := s.db.Joins("JOIN conversation_members own ON own.conversation_id=conversations.id AND own.user_id=?", id.UserID).Where("conversations.organization_id=?", id.OrganizationID).Preload("Members.User").Order("conversations.updated_at DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, conversation := range items {
		members := []gin.H{}
		for _, member := range conversation.Members {
			var membership domain.Membership
			var employee domain.Employee
			_ = s.db.Where("organization_id=? AND user_id=?", id.OrganizationID, member.UserID).First(&membership).Error
			if membership.EmployeeID != nil {
				_ = s.db.Preload("Position").First(&employee, "id=?", *membership.EmployeeID).Error
			}
			members = append(members, gin.H{"userId": member.UserID, "email": member.User.Email, "employeeId": membership.EmployeeID, "name": employee.FullName, "role": employee.Position.Title})
		}
		var last domain.Message
		_ = s.db.Where("conversation_id=?", conversation.ID).Order("created_at DESC").First(&last).Error
		result = append(result, gin.H{"id": conversation.ID, "title": conversation.Title, "members": members, "lastMessage": last, "updatedAt": conversation.UpdatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
func (s *Server) createConversation(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		Title   string   `json:"title"`
		UserIDs []string `json:"userIds" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Percakapan tidak valid", err.Error(), nil)
		return
	}
	userIDs := append(req.UserIDs, id.UserID)
	unique := map[string]bool{}
	conversation := domain.Conversation{OrganizationID: id.OrganizationID, Title: req.Title}
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Create(&conversation).Error; err != nil {
			return err
		}
		for _, userID := range userIDs {
			if unique[userID] {
				continue
			}
			unique[userID] = true
			var count int64
			tx.Model(&domain.Membership{}).Where("organization_id=? AND user_id=?", id.OrganizationID, userID).Count(&count)
			if count == 0 {
				return gorm.ErrRecordNotFound
			}
			if err := tx.Create(&domain.ConversationMember{OrganizationID: id.OrganizationID, ConversationID: conversation.ID, UserID: userID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, conversation)
}

func (s *Server) conversationAllowed(organizationID, userID, conversationID string) bool {
	var count int64
	s.db.Model(&domain.ConversationMember{}).Where("organization_id=? AND conversation_id=? AND user_id=?", organizationID, conversationID, userID).Count(&count)
	return count > 0
}
func (s *Server) listMessages(c *gin.Context) {
	id := identityFrom(c)
	if !s.conversationAllowed(id.OrganizationID, id.UserID, c.Param("id")) {
		problem(c, http.StatusForbidden, "conversation_denied", "Akses ditolak", "Anda bukan anggota percakapan ini.", nil)
		return
	}
	page, size := pagination(c)
	var items []domain.Message
	var total int64
	q := s.db.Model(&domain.Message{}).Where("organization_id=? AND conversation_id=?", id.OrganizationID, c.Param("id"))
	q.Count(&total)
	if err := q.Order("created_at ASC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}})
}
func (s *Server) createMessage(c *gin.Context) {
	id := identityFrom(c)
	conversationID := c.Param("id")
	if !s.conversationAllowed(id.OrganizationID, id.UserID, conversationID) {
		problem(c, http.StatusForbidden, "conversation_denied", "Akses ditolak", "Anda bukan anggota percakapan ini.", nil)
		return
	}
	var req struct {
		Body string `json:"body" binding:"required,min=1,max=4000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Pesan tidak valid", err.Error(), nil)
		return
	}
	item := domain.Message{OrganizationID: id.OrganizationID, ConversationID: conversationID, SenderUserID: id.UserID, Body: strings.TrimSpace(req.Body)}
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		if err := tx.Model(&domain.Conversation{}).Where("id=? AND organization_id=?", conversationID, id.OrganizationID).Update("updated_at", time.Now().UTC()).Error; err != nil {
			return err
		}
		return repository.AddOutbox(tx, id.OrganizationID, "notification:digest", gin.H{"organizationId": id.OrganizationID, "conversationId": conversationID, "messageId": item.ID})
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	event := gin.H{"type": "message.created", "data": item}
	s.hub.Publish(c, id.OrganizationID, event)
	c.JSON(http.StatusCreated, item)
}

func (s *Server) realtimeTicket(c *gin.Context) {
	id := identityFrom(c)
	// Realtime is an optional enhancement: every view also refreshes over REST. When
	// Redis is unreachable we answer 200 with `realtime:false` rather than an error,
	// so clients skip the socket instead of opening one that is doomed to fail. The
	// socket itself still answers 503 if a client tries anyway.
	if !s.redisAvailable(c.Request.Context()) {
		c.JSON(http.StatusOK, gin.H{"realtime": false, "ticket": "", "expiresIn": 0})
		return
	}
	token, err := s.tokens.Issue(secure.Claims{UserID: id.UserID, OrganizationID: id.OrganizationID, MembershipID: id.MembershipID, Role: id.Role, TokenType: "realtime"}, 60*time.Second)
	if err != nil {
		problem(c, http.StatusInternalServerError, "ticket_failed", "Tiket realtime gagal dibuat", err.Error(), nil)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"realtime": true, "ticket": token, "expiresIn": 60})
}
func (s *Server) websocket(c *gin.Context) {
	rawTicket := c.Query("ticket")
	claims, err := s.tokens.Parse(rawTicket, "realtime")
	if err != nil {
		problem(c, http.StatusUnauthorized, "invalid_realtime_ticket", "Tiket realtime tidak valid", "Minta tiket baru.", nil)
		return
	}
	if !s.redisAvailable(c.Request.Context()) {
		problem(c, http.StatusServiceUnavailable, "realtime_unavailable", "Realtime tidak tersedia", "Redis belum dapat dihubungi, pembaruan langsung dinonaktifkan sementara.", nil)
		return
	}
	accepted, err := s.redis.SetNX(c.Request.Context(), "simpul:realtime-ticket:"+secure.HashToken(rawTicket), "used", 60*time.Second).Result()
	if err != nil {
		// A failed SetNX here means Redis dropped out between the ping and the write.
		// That is an outage, not a bad ticket, and must not be reported as a 401.
		problem(c, http.StatusServiceUnavailable, "realtime_unavailable", "Realtime tidak tersedia", "Redis belum dapat dihubungi, pembaruan langsung dinonaktifkan sementara.", nil)
		return
	}
	if !accepted {
		problem(c, http.StatusUnauthorized, "realtime_ticket_used", "Tiket realtime tidak valid", "Tiket sudah dipakai. Minta tiket baru.", nil)
		return
	}
	s.hub.Serve(c, claims.OrganizationID)
}

func (s *Server) listFAQs(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.FAQ
	q := s.db.Where("organization_id=?", id.OrganizationID)
	if category := c.Query("category"); category != "" {
		q = q.Where("category=?", category)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("lower(question) LIKE ? OR lower(answer) LIKE ?", like, like)
	}
	if err := q.Order("sort_order,created_at").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) createSupportTicket(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		Subject  string `json:"subject" binding:"required,min=3,max=200"`
		Category string `json:"category" binding:"required"`
		Message  string `json:"message" binding:"required,min=5,max=5000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Tiket tidak valid", err.Error(), nil)
		return
	}
	item := domain.SupportTicket{OrganizationID: id.OrganizationID, RequesterID: id.UserID, Subject: req.Subject, Category: req.Category, Message: req.Message, Status: "open"}
	if err := s.db.Create(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}
func (s *Server) listSupportTickets(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.SupportTicket
	q := s.db.Where("organization_id=?", id.OrganizationID)
	if id.Role != "hr_admin" {
		q = q.Where("requester_id=?", id.UserID)
	}
	if err := q.Order("created_at DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
