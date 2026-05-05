package detect

import "testing"

func TestHostnameCheckForDefensiveInputs(t *testing.T) {
	check := HostnameCheckFor(nil)
	for _, input := range []string{
		"1.2.3.4",
		"api.example.com.",
		"",
		"localhost",
	} {
		if check([]byte(input)) {
			t.Fatalf("HostnameCheckFor accepted defensive rejection input %q", input)
		}
	}
}

func TestHostnameCheckForPSLAndExtras(t *testing.T) {
	if !HostnameCheckFor(nil)([]byte("xn--nxasmq6b.example.com")) {
		t.Fatal("HostnameCheckFor rejected punycode-shaped public hostname")
	}
	if HostnameCheckFor(nil)([]byte("db-1.prod.acme")) {
		t.Fatal("HostnameCheckFor accepted acme without configured internal TLD")
	}
	if !HostnameCheckFor([]string{"acme"})([]byte("db-1.prod.acme")) {
		t.Fatal("HostnameCheckFor rejected configured internal TLD")
	}
}
