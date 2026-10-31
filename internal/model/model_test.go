package model

import "testing"

func TestCatalogLoads(t *testing.T) {
	cat, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) < 10 {
		t.Fatalf("want >= 10 models, got %d", len(cat))
	}
	seen := map[string]bool{}
	for _, m := range cat {
		for _, k := range append([]string{m.ID}, m.Aliases...) {
			if seen[k] {
				t.Errorf("duplicate id/alias %q", k)
			}
			seen[k] = true
		}
	}
}

func TestResolve(t *testing.T) {
	m, err := Resolve("Llama3.1:8B")
	if err != nil || m.ID != "llama3.1-8b" {
		t.Fatalf("got %+v %v", m, err)
	}
	g, err := Resolve("13b")
	if err != nil || !g.Generic || g.ParamsB != 13 {
		t.Fatalf("generic: %+v %v", g, err)
	}
	if _, err := Resolve("not-a-model"); err == nil {
		t.Fatal("want error")
	}
	if _, err := Resolve("0b"); err == nil {
		t.Fatal("0b must be rejected")
	}
	if m, err := Resolve("llama3.1:latest"); err != nil || m.ID != "llama3.1-8b" {
		t.Fatalf(":latest: %+v %v", m, err)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse("id: x\nparams_b: 1\nlayers: 2\nkv_heads: 2\nhead_dim: 64\nkv_head: 3\n")
	if err == nil {
		t.Fatal("typo field must be an error")
	}
}
