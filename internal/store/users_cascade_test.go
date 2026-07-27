package store

import (
	"strings"
	"testing"
)

// TestDeleteUserCascade_RemovesPerUserRows verifies the cascade helper:
// every per-user table loses the user's rows, while audit_logs survive.
func TestDeleteUserCascade_RemovesPerUserRows(t *testing.T) {
	st := newTestStore(t)

	owner, err := st.CreateUser("owner", "x", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Seed per-user rows the cascade must clean.
	if _, err := st.CreateAirportForUser(owner.ID, "owner-airport", "https://example.com/sub"); err != nil {
		t.Fatalf("CreateAirportForUser: %v", err)
	}
	if _, err := st.CreateEndpointForUser(owner.ID, "owner-endpoint"); err != nil {
		t.Fatalf("CreateEndpointForUser: %v", err)
	}
	if err := st.UpsertUserQuota(&UserQuota{UserID: owner.ID, MaxAirports: 1}); err != nil {
		t.Fatalf("UpsertUserQuota: %v", err)
	}
	if err := st.CreateOrUpdateXrayInstance(&XrayInstance{
		UserID: owner.ID, Port: 31000, ConfigPath: "/tmp/x.json", Status: XrayStatusStopped,
	}); err != nil {
		t.Fatalf("CreateOrUpdateXrayInstance: %v", err)
	}

	// Audit row pre-delete: must survive the cascade.
	if err := st.RecordAuditEvent("login_success", "1.2.3.4", "owner", ""); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}

	if err := st.DeleteUserCascade(owner.ID); err != nil {
		t.Fatalf("DeleteUserCascade: %v", err)
	}

	if _, err := st.GetUserByID(owner.ID); err == nil {
		t.Error("user still present after DeleteUserCascade")
	}
	if _, err := st.GetUserQuota(owner.ID); err == nil {
		t.Error("quota still present after DeleteUserCascade")
	}
	if _, err := st.GetXrayInstanceByUserID(owner.ID); err == nil {
		t.Error("xray instance still present after DeleteUserCascade")
	}

	airports, err := st.ListAirports()
	if err != nil {
		t.Fatalf("ListAirports: %v", err)
	}
	for _, a := range airports {
		if a.UserID == owner.ID {
			t.Errorf("airport id=%d still owned by deleted user %d", a.ID, owner.ID)
		}
	}
	endpoints, err := st.ListEndpoints()
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	for _, ep := range endpoints {
		if ep.UserID == owner.ID {
			t.Errorf("endpoint id=%d still owned by deleted user %d", ep.ID, owner.ID)
		}
	}
}

// TestDeleteUserCascade_PreservesAuditLogs pins the deliberate exception:
// the audit trail survives account deletion.
func TestDeleteUserCascade_PreservesAuditLogs(t *testing.T) {
	st := newTestStore(t)

	owner, err := st.CreateUser("audit-target", "x", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.RecordAuditEvent("login_success", "1.2.3.4", "audit-target", ""); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}

	if err := st.DeleteUserCascade(owner.ID); err != nil {
		t.Fatalf("DeleteUserCascade: %v", err)
	}

	// Audit rows are keyed by username string, not user_id; even after the
	// user row is gone the trail must remain queryable.
	var count int
	row := st.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE username = ?`, "audit-target")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if count == 0 {
		t.Error("audit_logs rows removed by cascade; audit trail must survive user deletion")
	}
}

// TestDeleteUserCascade_RejectsZeroID pins the input validation.
func TestDeleteUserCascade_RejectsZeroID(t *testing.T) {
	st := newTestStore(t)
	if err := st.DeleteUserCascade(0); err == nil {
		t.Fatal("DeleteUserCascade(0) = nil, want ErrInvalidInput")
	} else if !strings.Contains(err.Error(), ErrInvalidInput.Error()) {
		t.Errorf("DeleteUserCascade(0) error = %v, want wrap of ErrInvalidInput", err)
	}
	if err := st.DeleteUserCascade(-3); err == nil {
		t.Fatal("DeleteUserCascade(-3) = nil, want ErrInvalidInput")
	}
}
