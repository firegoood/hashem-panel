package main

import (
	"reflect"
	"testing"
)

func TestBundleMakeAndParse(t *testing.T) {
	// Test standard bundle with ports and fou ports (7 parts)
	bundle7 := "hsh1_85.1.2.3_7000_10.10.10.2_10.10.10.1_mytoken123_443-2083_fou443-55555"
	b7, err := ParseBundle(bundle7)
	if err != nil {
		t.Fatalf("ParseBundle(7 parts) failed: %v", err)
	}
	if b7.IranPub != "85.1.2.3" || b7.FrpPort != 7000 || b7.Token != "mytoken123" {
		t.Fatalf("unexpected fields in b7: %+v", b7)
	}
	if !reflect.DeepEqual(b7.Ports, []int{443, 2083}) {
		t.Fatalf("unexpected ports in b7: %+v", b7.Ports)
	}
	if !reflect.DeepEqual(b7.FouPorts, []int{443, 55555}) {
		t.Fatalf("unexpected fou_ports in b7: %+v", b7.FouPorts)
	}

	// Test bundle without ports but with fou (7 parts with empty ports: __fou...)
	bundleNoPorts := "hsh1_85.1.2.3_7000_10.10.10.2_10.10.10.1_mytoken123__fou443-55555"
	bNP, err := ParseBundle(bundleNoPorts)
	if err != nil {
		t.Fatalf("ParseBundle(empty ports) failed: %v", err)
	}
	if len(bNP.Ports) != 0 {
		t.Fatalf("expected 0 ports in bNP, got %+v", bNP.Ports)
	}
	if !reflect.DeepEqual(bNP.FouPorts, []int{443, 55555}) {
		t.Fatalf("unexpected fou_ports in bNP: %+v", bNP.FouPorts)
	}

	// Test legacy 5 parts bundle
	bundle5 := "hsh1_85.1.2.3_7000_10.10.10.2_10.10.10.1_mytoken123"
	b5, err := ParseBundle(bundle5)
	if err != nil {
		t.Fatalf("ParseBundle(5 parts) failed: %v", err)
	}
	if b5.Token != "mytoken123" || len(b5.Ports) != 0 || len(b5.FouPorts) != 0 {
		t.Fatalf("unexpected fields in b5: %+v", b5)
	}

	// Test legacy 6 parts bundle with ports
	bundle6 := "hsh1_85.1.2.3_7000_10.10.10.2_10.10.10.1_mytoken123_443-8080"
	b6, err := ParseBundle(bundle6)
	if err != nil {
		t.Fatalf("ParseBundle(6 parts) failed: %v", err)
	}
	if !reflect.DeepEqual(b6.Ports, []int{443, 8080}) {
		t.Fatalf("unexpected ports in b6: %+v", b6.Ports)
	}

	// Test MakeBundle generates valid parseable string
	made := MakeBundle("85.1.2.3", 7000, "10.10.10.2", "10.10.10.1", "mytoken123", []int{443, 2083})
	bMade, err := ParseBundle(made)
	if err != nil {
		t.Fatalf("ParseBundle(MakeBundle output) failed: %v, bundle was: %s", err, made)
	}
	if bMade.IranPub != "85.1.2.3" || !reflect.DeepEqual(bMade.Ports, []int{443, 2083}) {
		t.Fatalf("mismatch in bMade: %+v", bMade)
	}

	// Regression: exact bundle from user report (no ports, has fou)
	userBundle := "hsh1_85.198.48.162_56261_10.10.10.2_10.10.10.1_sHwurcbG926fk4tLcexTF0MXzcNxoHkW__fou443-55555"
	bUser, err := ParseBundle(userBundle)
	if err != nil {
		t.Fatalf("ParseBundle(userBundle) failed: %v", err)
	}
	if bUser.IranPub != "85.198.48.162" || bUser.FrpPort != 56261 {
		t.Fatalf("unexpected user bundle fields: %+v", bUser)
	}
	if bUser.Token != "sHwurcbG926fk4tLcexTF0MXzcNxoHkW" {
		t.Fatalf("wrong token in user bundle: %q", bUser.Token)
	}
	if len(bUser.Ports) != 0 {
		t.Fatalf("user bundle should have no ports, got %+v", bUser.Ports)
	}
	if !reflect.DeepEqual(bUser.FouPorts, []int{443, 55555}) {
		t.Fatalf("wrong fou ports in user bundle: %+v", bUser.FouPorts)
	}
}

// TestApplyBundlePreservesUserPorts verifies that when a bundle has no ports,
// applyBundle does not override any ports the user already typed.
func TestApplyBundlePreservesUserPorts(t *testing.T) {
	// Bundle with no ports segment
	bundleNoPorts := "hsh1_85.198.48.162_56261_10.10.10.2_10.10.10.1_sHwurcbG926fk4tLcexTF0MXzcNxoHkW__fou443-55555"
	b, err := ParseBundle(bundleNoPorts)
	if err != nil {
		t.Fatalf("ParseBundle failed: %v", err)
	}

	// Simulate user having typed "443, 2083" in the Reverse Ports field
	body := setupRequest{
		Role:  "foreign",
		Ports: "443, 2083",
		Token: bundleNoPorts, // the bundle string before applyBundle
	}
	body.OrigBundle = body.Token
	applyBundle(&body, b)

	// Ports must be preserved from user input (not wiped by empty bundle ports)
	if body.Ports != "443, 2083" {
		t.Fatalf("applyBundle wiped user ports: got %q, want %q", body.Ports, "443, 2083")
	}
	// Token must now be the inner token
	if body.Token != "sHwurcbG926fk4tLcexTF0MXzcNxoHkW" {
		t.Fatalf("wrong inner token after applyBundle: %q", body.Token)
	}
	// OrigBundle must be preserved
	if body.OrigBundle != bundleNoPorts {
		t.Fatalf("OrigBundle was changed: %q", body.OrigBundle)
	}
	// GRE addresses must be filled from bundle
	if body.RemotePub != "85.198.48.162" {
		t.Fatalf("RemotePub not filled from bundle: %q", body.RemotePub)
	}
}
