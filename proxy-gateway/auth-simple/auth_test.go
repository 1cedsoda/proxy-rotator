package simple

import "testing"

func TestCorrectCredentials(t *testing.T) {
	p := New("alice", "s3cret")
	if err := p.Authenticate("alice", "s3cret"); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestWrongPassword(t *testing.T) {
	p := New("alice", "s3cret")
	if err := p.Authenticate("alice", "wrong"); err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestWrongSub(t *testing.T) {
	p := New("alice", "s3cret")
	if err := p.Authenticate("bob", "s3cret"); err == nil {
		t.Fatal("expected error for wrong sub")
	}
}

func TestBothWrong(t *testing.T) {
	p := New("alice", "s3cret")
	if err := p.Authenticate("bob", "wrong"); err == nil {
		t.Fatal("expected error for wrong sub and password")
	}
}

func TestEmptySub(t *testing.T) {
	p := New("alice", "s3cret")
	if err := p.Authenticate("", "s3cret"); err == nil {
		t.Fatal("expected error for empty sub")
	}
}

func TestEmptyPassword(t *testing.T) {
	p := New("alice", "s3cret")
	if err := p.Authenticate("alice", ""); err == nil {
		t.Fatal("expected error for empty password")
	}
}
