package jobs

import (
	"testing"

	"github.com/hibiken/asynq"
)

func TestOrganizationTaskPayload(t *testing.T) {
	organizationID, err := organizationFromTask(asynq.NewTask(TypeLeaveAccrual, []byte(`{"organizationId":"org-1"}`)))
	if err != nil || organizationID != "org-1" {
		t.Fatalf("unexpected payload result: id=%q err=%v", organizationID, err)
	}
	if _, err := organizationFromTask(asynq.NewTask(TypeLeaveAccrual, []byte(`{}`))); err == nil {
		t.Fatal("missing organizationId must be rejected")
	}
}
