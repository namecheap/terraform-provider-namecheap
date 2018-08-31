package namecheap

import (
	"reflect"
	"testing"
)

func TestNS__GetNS(t *testing.T) {
	if !clientEnabled {
		t.Skip("namecheap credentials not configured")
	}

	ns, err := testClient.GetNS(testDomain)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 2 {
		t.Errorf("got %d records", len(ns))
	}
	ans := []string{
		"dns1.registrar-servers.com",
		"dns2.registrar-servers.com",
	}
	if !reflect.DeepEqual(ns, ans) {
		t.Errorf("got %q", ns)
	}
}
