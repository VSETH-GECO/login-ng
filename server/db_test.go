package server

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/rs/zerolog"
)

// newTestServer builds a Server backed by a sqlmock DB.
func newTestServer(t *testing.T) (Server, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	s := Server{Log: zerolog.Nop(), DB: db{sqlDB}}
	return s, mock
}

// ── locateUser ────────────────────────────────────────────────────────────────

func TestLocateUser_found(t *testing.T) {
	s, mock := newTestServer(t)

	rows := sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
		AddRow("127.0.0.1", "aabbccddeeff", "10.0.0.1")
	mock.ExpectQuery("SELECT").WithArgs("127.0.0.1").WillReturnRows(rows)

	up, err := s.locateUser(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if up.userIP != "127.0.0.1" || up.userMAC != "aabbccddeeff" || up.switchIP != "10.0.0.1" {
		t.Errorf("unexpected userProperties: %+v", up)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestLocateUser_notFound(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectQuery("SELECT").WithArgs("1.2.3.4").WillReturnError(sql.ErrNoRows)

	_, err := s.locateUser(context.Background(), "1.2.3.4")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "user not found" {
		t.Errorf("unexpected error message: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestLocateUser_dbError(t *testing.T) {
	s, mock := newTestServer(t)

	dbErr := errors.New("connection refused")
	mock.ExpectQuery("SELECT").WithArgs("1.2.3.4").WillReturnError(dbErr)

	_, err := s.locateUser(context.Background(), "1.2.3.4")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── createNewBounceJob ────────────────────────────────────────────────────────

func TestCreateNewBounceJob_success(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectExec("INSERT INTO bouncer_jobs").
		WithArgs("aabbcc", 527).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.createNewBounceJob(context.Background(), "aabbcc", 527); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCreateNewBounceJob_dbError(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectExec("INSERT INTO bouncer_jobs").
		WithArgs("aabbcc", 527).
		WillReturnError(errors.New("deadlock"))

	if err := s.createNewBounceJob(context.Background(), "aabbcc", 527); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── createNewLoginLog ─────────────────────────────────────────────────────────

func TestCreateNewLoginLog_success(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectExec("INSERT INTO login_logs").
		WithArgs("alice", "aabbcc").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.createNewLoginLog(context.Background(), "alice", "aabbcc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCreateNewLoginLog_dbError(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectExec("INSERT INTO login_logs").
		WithArgs("alice", "aabbcc").
		WillReturnError(errors.New("disk full"))

	if err := s.createNewLoginLog(context.Background(), "alice", "aabbcc"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── getSwitchVLAN ─────────────────────────────────────────────────────────────

func TestGetSwitchVLAN_found(t *testing.T) {
	s, mock := newTestServer(t)

	rows := sqlmock.NewRows([]string{"vlan"}).AddRow(527)
	mock.ExpectQuery("SELECT primary_vlan").WithArgs("10.0.0.1").WillReturnRows(rows)

	vlan, err := s.getSwitchVLAN(context.Background(), "10.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vlan != 527 {
		t.Errorf("expected 527, got %d", vlan)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestGetSwitchVLAN_notFound(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectQuery("SELECT primary_vlan").WithArgs("10.0.0.1").WillReturnError(sql.ErrNoRows)

	_, err := s.getSwitchVLAN(context.Background(), "10.0.0.1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "vlan not found" {
		t.Errorf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── getAvailableVLANs ─────────────────────────────────────────────────────────

func TestGetAvailableVLANs_success(t *testing.T) {
	s, mock := newTestServer(t)

	rows := sqlmock.NewRows([]string{"name", "vlan_id"}).
		AddRow("Shared 1", 490).
		AddRow("Shared 2", 491).
		AddRow("User 27", 527)
	mock.ExpectQuery("SELECT name, vlan_id").WithArgs(527).WillReturnRows(rows)

	vlans, err := s.getAvailableVLANs(context.Background(), 527)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vlans) != 3 {
		t.Fatalf("expected 3 VLANs, got %d", len(vlans))
	}
	if vlans[2].VLANID != 527 || vlans[2].Name != "User 27" {
		t.Errorf("unexpected last VLAN: %+v", vlans[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestGetAvailableVLANs_empty(t *testing.T) {
	s, mock := newTestServer(t)

	rows := sqlmock.NewRows([]string{"name", "vlan_id"})
	mock.ExpectQuery("SELECT name, vlan_id").WithArgs(999).WillReturnRows(rows)

	vlans, err := s.getAvailableVLANs(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vlans) != 0 {
		t.Errorf("expected 0 VLANs, got %d", len(vlans))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestGetAvailableVLANs_dbError(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectQuery("SELECT name, vlan_id").WithArgs(527).WillReturnError(errors.New("timeout"))

	_, err := s.getAvailableVLANs(context.Background(), 527)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── getSwitchVLAN — generic DB error ─────────────────────────────────────────

func TestGetSwitchVLAN_dbError(t *testing.T) {
	s, mock := newTestServer(t)

	mock.ExpectQuery("SELECT primary_vlan").WithArgs("10.0.0.1").WillReturnError(errors.New("connection reset"))

	_, err := s.getSwitchVLAN(context.Background(), "10.0.0.1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// should be wrapped, not the sentinel "vlan not found"
	if err.Error() == "vlan not found" {
		t.Errorf("expected wrapped db error, got sentinel: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── getAvailableVLANs — rows.Err() path ──────────────────────────────────────

func TestGetAvailableVLANs_rowsErr(t *testing.T) {
	s, mock := newTestServer(t)

	// Return one good row then trigger a rows.Err() via a forced close error.
	// go-sqlmock v1 supports RowError to simulate mid-iteration failure.
	rows := sqlmock.NewRows([]string{"name", "vlan_id"}).
		AddRow("Shared 1", 490).
		RowError(0, errors.New("network interrupted"))
	mock.ExpectQuery("SELECT name, vlan_id").WithArgs(527).WillReturnRows(rows)

	_, err := s.getAvailableVLANs(context.Background(), 527)
	if err == nil {
		t.Fatal("expected error from rows.Err(), got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
