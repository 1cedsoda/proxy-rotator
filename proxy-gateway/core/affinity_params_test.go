package core

import (
	"testing"
)

func TestAffinityParamsEmpty(t *testing.T) {
	p := NewAffinityParams()
	if len(p) != 0 {
		t.Fatal("expected empty")
	}
}

func TestAffinityParamsValidStringNumber(t *testing.T) {
	raw := map[string]interface{}{"app": "myapp", "count": float64(42)}
	p, err := ParseAffinityParams(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.GetString("app") != "myapp" {
		t.Fatal("expected myapp")
	}
}

func TestAffinityParamsRejectsBool(t *testing.T) {
	_, err := ParseAffinityParams(map[string]interface{}{"flag": true})
	if err == nil {
		t.Fatal("expected error for bool")
	}
}

func TestAffinityParamsRejectsNull(t *testing.T) {
	_, err := ParseAffinityParams(map[string]interface{}{"x": nil})
	if err == nil {
		t.Fatal("expected error for null")
	}
}

func TestAffinityParamsRejectsArray(t *testing.T) {
	_, err := ParseAffinityParams(map[string]interface{}{"list": []interface{}{1, 2}})
	if err == nil {
		t.Fatal("expected error for array")
	}
}

func TestAffinityParamsRejectsNestedObject(t *testing.T) {
	_, err := ParseAffinityParams(map[string]interface{}{"nested": map[string]interface{}{}})
	if err == nil {
		t.Fatal("expected error for nested object")
	}
}
