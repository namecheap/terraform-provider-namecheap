package namecheap

import (
	"os"
	"testing"
)

var (
	testDomain string
	testRecord *Record

	testClient, _ = New()

	// check this once -- used in tests
	clientEnabled = testClient != nil && os.Getenv("MOCKED") == ""
)

func init() {
	testDomain = "dsm.rent"
	testRecord = &Record{
		Name:               "www",
		FriendlyName:       "CNAME Record",
		Address:            "parkingpage.namecheap.com.",
		MXPref:             10,
		AssociatedAppTitle: "",
		Id:                 92111926,
		RecordType:         "CNAME",
		TTL:                1800,
		IsActive:           true,
		IsDDNSEnabled:      false,
	}
}

func TestClient__fail(t *testing.T) {
	if !clientEnabled {
		t.Skip("namecheap credentials not configured")
	}

	cases := []struct {
		username, apiuser, token, ip string
	}{
		{
			username: "",
			apiuser:  "apiuser",
			token:    "token",
			ip:       "127.0.0.1",
		},
		{
			username: "username",
			apiuser:  "",
			token:    "token",
			ip:       "127.0.0.1",
		},
		{
			username: "username",
			apiuser:  "apiuser",
			token:    "",
			ip:       "127.0.0.1",
		},
		{
			username: "username",
			apiuser:  "apiuser",
			token:    "token",
			ip:       "",
		},
	}
	for i := range cases {
		_, err := NewClient(cases[i].username, cases[i].apiuser, cases[i].token, cases[i].ip, false)
		if err == nil {
			t.Errorf("expected error, %q %q %q %q", cases[i].username, cases[i].apiuser, cases[i].token, cases[i].ip)
		}
	}
}

func TestClient_NewRequest(t *testing.T) {
	testClient, _ := NewClient("user", "apiuser", "secret", "128.0.0.1", true)

	body := map[string]string{
		"foo": "bar",
		"baz": "bar",
	}
	req, err := testClient.NewRequest(body)
	if err != nil {
		t.Fatalf("bad: %v", err)
	}

	if req.URL.String() != "https://api.sandbox.namecheap.com/xml.response" {
		t.Fatalf("bad base url: %v", req.URL.String())
	}

	if req.Method != "POST" {
		t.Fatalf("bad method: %v", req.Method)
	}
}
