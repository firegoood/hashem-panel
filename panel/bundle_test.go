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
}
