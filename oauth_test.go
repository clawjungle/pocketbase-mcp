package pbmcp

import "testing"

func TestVerifyPKCE(t *testing.T) {
	// RFC 7636 §4.1 example verifier -> S256 challenge.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	if !verifyPKCE(challenge, verifier) {
		t.Fatal("expected matching verifier/challenge pair to verify")
	}
	if verifyPKCE(challenge, "wrong-verifier") {
		t.Fatal("expected mismatched verifier to fail")
	}
	if verifyPKCE("", verifier) || verifyPKCE(challenge, "") {
		t.Fatal("expected empty challenge or verifier to fail")
	}
}

func TestRpcMessageIsNotification(t *testing.T) {
	withID := rpcMessage{Method: "ping", ID: []byte(`1`)}
	if withID.isNotification() {
		t.Fatal("message with an id should not be a notification")
	}

	notif := rpcMessage{Method: "notifications/initialized"}
	if !notif.isNotification() {
		t.Fatal("message with no id should be a notification")
	}
}
