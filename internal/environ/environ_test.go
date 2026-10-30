package environ

import (
	"reflect"
	"testing"
)

func TestSetReplacesExistingValues(t *testing.T) {
	got := Set([]string{"HOME=/h", "FOO=old", "FOOBAR=keep"}, "FOO=new", "NEW=1")
	want := []string{"HOME=/h", "FOOBAR=keep", "FOO=new", "NEW=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Set = %q, want %q", got, want)
	}
}

func TestSetLeavesBaseUntouched(t *testing.T) {
	base := []string{"FOO=old", "HOME=/h"}
	Set(base, "FOO=new")
	if want := []string{"FOO=old", "HOME=/h"}; !reflect.DeepEqual(base, want) {
		t.Errorf("base changed to %q", base)
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"HF_TOKEN", "_x", "a1"} {
		if !ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "1BAD", "HAS-DASH", "A=B", "two words"} {
		if ValidName(name) {
			t.Errorf("ValidName(%q) = true, want false", name)
		}
	}
}
