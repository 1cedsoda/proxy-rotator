package core

import "testing"

func TestParseUsernameJSONValid(t *testing.T) {
	j := `{"sub":"alice","set":"residential","minutes":5,"meta":{"app":"myapp"}}`
	p, err := ParseUsernameJSON(j)
	if err != nil {
		t.Fatal(err)
	}
	if p.Sub != "alice" {
		t.Fatalf("expected sub=alice, got %s", p.Sub)
	}
	if p.SetName != "residential" {
		t.Fatalf("expected set=residential, got %s", p.SetName)
	}
	if p.AffinityMinutes != 5 {
		t.Fatalf("expected minutes=5, got %d", p.AffinityMinutes)
	}
	if p.AffinityParams.GetString("app") != "myapp" {
		t.Fatal("expected meta.app=myapp")
	}
}

func TestParseUsernameJSONMissingSub(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"set":"residential","minutes":5,"meta":{}}`); err == nil {
		t.Fatal("expected error for missing sub")
	}
}

func TestParseUsernameJSONEmptySub(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"","set":"residential","minutes":5,"meta":{}}`); err == nil {
		t.Fatal("expected error for empty sub")
	}
}

func TestParseUsernameJSONMissingSet(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","minutes":5,"meta":{}}`); err == nil {
		t.Fatal("expected error for missing set")
	}
}

func TestParseUsernameJSONMissingMinutes(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","meta":{}}`); err == nil {
		t.Fatal("expected error for missing minutes")
	}
}

func TestParseUsernameJSONMissingMeta(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":5}`); err == nil {
		t.Fatal("expected error for missing meta")
	}
}

func TestParseUsernameJSONExtraKey(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":5,"meta":{},"extra":"x"}`); err == nil {
		t.Fatal("expected error for extra key")
	}
}

func TestParseUsernameJSONMinutesTooHigh(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":1441,"meta":{}}`); err == nil {
		t.Fatal("expected error for minutes > 1440")
	}
}

func TestParseUsernameJSONMinutesFloat(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":5.5,"meta":{}}`); err == nil {
		t.Fatal("expected error for float minutes")
	}
}

func TestParseUsernameJSONMinutesString(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":"5","meta":{}}`); err == nil {
		t.Fatal("expected error for string minutes")
	}
}

func TestParseUsernameJSONSetNonAlphanumeric(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"resi-dential","minutes":5,"meta":{}}`); err == nil {
		t.Fatal("expected error for non-alphanumeric set")
	}
}

func TestParseUsernameJSONMetaNestedObject(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":5,"meta":{"a":{"b":1}}}`); err == nil {
		t.Fatal("expected error for nested object in meta")
	}
}

func TestParseUsernameJSONMetaBoolean(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":5,"meta":{"flag":true}}`); err == nil {
		t.Fatal("expected error for boolean in meta")
	}
}

func TestParseUsernameJSONAnyKeyOrder(t *testing.T) {
	if _, err := ParseUsernameJSON(`{"minutes":0,"meta":{},"set":"test","sub":"bob"}`); err != nil {
		t.Fatalf("any key order should be accepted: %v", err)
	}
}

func TestParseUsernameJSONZeroMinutes(t *testing.T) {
	p, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":0,"meta":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.AffinityMinutes != 0 {
		t.Fatal("expected 0 minutes")
	}
}

func TestParseUsernameJSONMaxMinutes(t *testing.T) {
	p, err := ParseUsernameJSON(`{"sub":"alice","set":"residential","minutes":1440,"meta":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.AffinityMinutes != 1440 {
		t.Fatalf("expected 1440, got %d", p.AffinityMinutes)
	}
}
