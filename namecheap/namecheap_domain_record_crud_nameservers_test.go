package namecheap_provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateNameserversMerge_OnDefaultDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			assert.Contains(t, r.FormValue("Nameservers"), "ns1.example.com")
			assert.Contains(t, r.FormValue("Nameservers"), "ns2.example.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		default:
			t.Fatalf("unexpected command: %s", command)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns1.example.com", "ns2.example.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.False(t, diags.HasError())
}

func TestCreateNameserversMerge_MergesWithExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.existing.com", "ns2.existing.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns1.existing.com")
			assert.Contains(t, ns, "ns2.existing.com")
			assert.Contains(t, ns, "ns3.new.com")
			assert.Contains(t, ns, "ns4.new.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns3.new.com", "ns4.new.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.False(t, diags.HasError())
}

func TestCreateNameserversMerge_DuplicateDetection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com"}))
		default:
			t.Fatalf("should not call setCustom on duplicate: %s", command)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns1.example.com", "ns3.example.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Detail, "ns1.example.com")
	assert.Contains(t, diags[0].Detail, "already exist")
}

func TestCreateNameserversMerge_DuplicateCaseInsensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	nameservers := []string{"ns1.example.com", "ns3.example.com"}

	diags := createNameserversMerge("test.com", nameservers, client)
	assert.True(t, diags.HasError())
}

func TestCreateNameserversOverwrite_Simple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setCustom", r.FormValue("Command"))
		_, _ = fmt.Fprint(w, setCustomSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversOverwrite("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
}

func TestReadNameserversMerge_FindsMatching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com", "ns3.other.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversMerge_CaseInsensitiveMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"NS1.EXAMPLE.COM", "NS2.EXAMPLE.COM"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com"}, *result)
}

func TestReadNameserversMerge_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(true, nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameserversMerge_NoMatchFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.other.com", "ns2.other.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestReadNameserversOverwrite_ReturnsAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.example.com", "ns2.example.com"}))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.Equal(t, []string{"ns1.example.com", "ns2.example.com"}, *result)
}

func TestReadNameserversOverwrite_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, getListXML(true, nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.Empty(t, *result)
}

func TestUpdateNameserversMerge_ReplacesOldWithNew(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com", "ns3.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns3.manual.com")
			assert.Contains(t, ns, "ns1.new.com")
			assert.Contains(t, ns, "ns2.new.com")
			assert.NotContains(t, ns, "ns1.old.com")
			assert.NotContains(t, ns, "ns2.old.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{"ns1.new.com", "ns2.new.com"}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.False(t, diags.HasError())
}

func TestUpdateNameserversMerge_OnlyOneRemains_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.manual.com"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	// Removing ns1.old.com leaves only ns2.manual.com (1 NS), which is invalid
	prev := []string{"ns1.old.com"}
	current := []string{}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "one remaining nameserver")
}

func TestUpdateNameserversMerge_ZeroRemains_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com"}))
		case "namecheap.domains.dns.setDefault":
			setDefaultCalled = true
			_, _ = fmt.Fprint(w, setDefaultSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.False(t, diags.HasError())
	assert.True(t, setDefaultCalled)
}

func TestUpdateNameserversMerge_UsingOurDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{}
	current := []string{"ns1.new.com", "ns2.new.com"}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.False(t, diags.HasError())
}

func TestDeleteNameserversMerge_RemovesManaged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com", "ns3.manual.com", "ns4.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			ns := r.FormValue("Nameservers")
			assert.Contains(t, ns, "ns3.manual.com")
			assert.Contains(t, ns, "ns4.manual.com")
			assert.NotContains(t, ns, "ns1.managed.com")
			assert.NotContains(t, ns, "ns2.managed.com")
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.False(t, diags.HasError())
}

func TestDeleteNameserversMerge_OneRemains_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.manual.com"}))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com"}, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "one remaining nameserver")
}

func TestDeleteNameserversMerge_AllRemoved_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com"}))
		case "namecheap.domains.dns.setDefault":
			setDefaultCalled = true
			_, _ = fmt.Fprint(w, setDefaultSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.False(t, diags.HasError())
	assert.True(t, setDefaultCalled)
}

func TestDeleteNameserversMerge_UsingOurDNS_Noop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		default:
			t.Fatalf("unexpected command when using our DNS: %s", command)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.False(t, diags.HasError())
}

func TestDeleteNameserversOverwrite_SetsDefault(t *testing.T) {
	var setDefaultCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "namecheap.domains.dns.setDefault", r.FormValue("Command"))
		setDefaultCalled = true
		_, _ = fmt.Fprint(w, setDefaultSuccessXML())
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.True(t, setDefaultCalled)
}

// TestNameserversMerge_SimpleAPIErrors consolidates simple error-path tests where
// the server returns HTTP 500 and the function under test is expected to return an error.
func TestNameserversMerge_SimpleAPIErrors(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, serverURL string)
	}{
		{
			name: "CreateNameserversMerge_GetListAPIError",
			fn: func(t *testing.T, serverURL string) {
				client := newTestClient(serverURL)
				diags := createNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
				assert.True(t, diags.HasError())
			},
		},
		{
			name: "ReadNameserversMerge_APIError",
			fn: func(t *testing.T, serverURL string) {
				client := newTestClient(serverURL)
				result, diags := readNameserversMerge("test.com", []string{"ns1.example.com"}, client)
				assert.True(t, diags.HasError())
				assert.Nil(t, result)
			},
		},
		{
			name: "UpdateNameserversMerge_GetListAPIError",
			fn: func(t *testing.T, serverURL string) {
				client := newTestClient(serverURL)
				diags := updateNameserversMerge("test.com", []string{"ns1.old.com"}, []string{"ns1.new.com", "ns2.new.com"}, client)
				assert.True(t, diags.HasError())
			},
		},
		{
			name: "DeleteNameserversMerge_GetListAPIError",
			fn: func(t *testing.T, serverURL string) {
				client := newTestClient(serverURL)
				diags := deleteNameserversMerge("test.com", []string{"ns1.example.com"}, client)
				assert.True(t, diags.HasError())
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(w, "internal error")
			}))
			defer server.Close()

			tc.fn(t, server.URL)
		})
	}
}

// TestNameserversOverwrite_SimpleAPIErrors consolidates simple error-path tests for
// overwrite-mode nameserver functions where the server returns an error response.
func TestNameserversOverwrite_SimpleAPIErrors(t *testing.T) {
	tests := []struct {
		name     string
		serverFn func(w http.ResponseWriter, r *http.Request)
		assertFn func(t *testing.T, serverURL string)
	}{
		{
			name: "CreateNameserversOverwrite_SetCustomAPIError",
			serverFn: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
			},
			assertFn: func(t *testing.T, serverURL string) {
				client := newTestClient(serverURL)
				diags := createNameserversOverwrite("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
				assert.True(t, diags.HasError())
			},
		},
		{
			name: "ReadNameserversOverwrite_GetListAPIError",
			serverFn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(w, "internal error")
			},
			assertFn: func(t *testing.T, serverURL string) {
				client := newTestClient(serverURL)
				result, diags := readNameserversOverwrite("test.com", client)
				assert.True(t, diags.HasError())
				assert.Nil(t, result)
			},
		},
		{
			name: "DeleteNameserversOverwrite_SetDefaultAPIError",
			serverFn: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
			},
			assertFn: func(t *testing.T, serverURL string) {
				client := newTestClient(serverURL)
				diags := deleteNameserversOverwrite("test.com", client)
				assert.True(t, diags.HasError())
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tc.serverFn))
			defer server.Close()

			tc.assertFn(t, server.URL)
		})
	}
}

func TestReadNameserversOverwrite_NilNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return custom DNS (IsUsingOurDNS=false) but with no nameserver elements
		_, _ = fmt.Fprint(w, getListXML(false, nil))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, diags := readNameserversOverwrite("test.com", client)
	assert.False(t, diags.HasError())
	assert.NotNil(t, result)
	// With nil nameservers and IsUsingOurDNS=false, should return empty list
	assert.Empty(t, *result)
}

func TestDeleteNameserversMerge_NilNameservers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			// Custom DNS but no nameservers in response
			_, _ = fmt.Fprint(w, getListXML(false, nil))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.example.com"}, client)
	assert.True(t, diags.HasError())
	assert.Contains(t, diags[0].Summary, "Invalid nameservers response")
}

func TestDeleteNameserversMerge_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com", "ns3.manual.com", "ns4.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.True(t, diags.HasError())
}

func TestDeleteNameserversMerge_SetDefaultAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.managed.com", "ns2.managed.com"}))
		case "namecheap.domains.dns.setDefault":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := deleteNameserversMerge("test.com", []string{"ns1.managed.com", "ns2.managed.com"}, client)
	assert.True(t, diags.HasError())
}

func TestUpdateNameserversMerge_SetDefaultAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com"}))
		case "namecheap.domains.dns.setDefault":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetDefault failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.True(t, diags.HasError())
}

func TestUpdateNameserversMerge_SetCustomAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.old.com", "ns2.old.com", "ns3.manual.com"}))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	prev := []string{"ns1.old.com", "ns2.old.com"}
	current := []string{"ns1.new.com", "ns2.new.com"}

	diags := updateNameserversMerge("test.com", prev, current, client)
	assert.True(t, diags.HasError())
}

func TestCreateNameserversMerge_SetCustomAPIError_OnDefaultDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(true, nil))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.example.com", "ns2.example.com"}, client)
	assert.True(t, diags.HasError())
}

func TestCreateNameserversMerge_SetCustomAPIError_OnCustomDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			_, _ = fmt.Fprint(w, getListXML(false, []string{"ns1.existing.com", "ns2.existing.com"}))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, apiErrorXML("500", "SetCustom failed"))
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns3.new.com", "ns4.new.com"}, client)
	assert.True(t, diags.HasError())
}

func TestCreateNameserversMerge_NilNameserversInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		command := r.FormValue("Command")

		switch command {
		case "namecheap.domains.dns.getList":
			// Custom DNS but no nameserver elements
			_, _ = fmt.Fprint(w, getListXML(false, nil))
		case "namecheap.domains.dns.setCustom":
			_, _ = fmt.Fprint(w, setCustomSuccessXML())
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	diags := createNameserversMerge("test.com", []string{"ns1.new.com", "ns2.new.com"}, client)
	assert.False(t, diags.HasError())
}
