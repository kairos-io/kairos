package main

import (
	"reflect"
	"testing"
)

func TestEnumSliceSetAccumulates(t *testing.T) {
	allowed := []string{"install", "init", "initrd"}
	cases := []struct {
		name string
		sets []string
		want []string
	}{
		{"comma separated", []string{"install,init"}, []string{"install", "init"}},
		{"repeated flag", []string{"install", "init"}, []string{"install", "init"}},
		{"mixed", []string{"install,init", "initrd"}, []string{"install", "init", "initrd"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnumSliceFlag(allowed, []string{})
			for _, s := range c.sets {
				if err := e.Set(s); err != nil {
					t.Fatalf("Set(%q): %v", s, err)
				}
			}
			if !reflect.DeepEqual(e.Value, c.want) {
				t.Fatalf("got %v, want %v", e.Value, c.want)
			}
		})
	}
}

func TestEnumSliceSetRejectsUnknownValue(t *testing.T) {
	e := newEnumSliceFlag([]string{"install"}, []string{})
	if err := e.Set("nope"); err == nil {
		t.Fatal("expected an error for a value that is not allowed")
	}
	if len(e.Value) != 0 {
		t.Fatalf("a rejected value must not be stored, got %v", e.Value)
	}
}
