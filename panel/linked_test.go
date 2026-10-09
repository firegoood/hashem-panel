package main

import "testing"

func TestLinkedFromSS(t *testing.T) {
	ss := "0 0 10.10.10.1:7091 10.10.10.2:51000\n0 0 192.0.2.10:7093 192.0.2.20:40000\n0 0 [::ffff:192.0.2.30]:22 192.0.2.40:5\n"
	if !linkedFromSS(ss, []int{7091, 7093, 7089}, "192.0.2.10", "10.10.10.2") {
		t.Fatal("GRE-inner session must count")
	}
	if linkedFromSS(ss, []int{7091}, "192.0.2.99", "10.99.0.2") {
		t.Fatal("foreign remote must not match")
	}
	if !linkedFromSS(ss, []int{7093}) {
		t.Fatal("no remote filter must match any client")
	}
	if linkedFromSS("", []int{7091}) {
		t.Fatal("empty ss output is not linked")
	}
}

func TestFleetRegisteredConnectionWithoutICMPIsHealthy(t *testing.T) {
	l := mkLive(1, true, true, false, "")
	l.Linked = true
	l.Managed, l.Authenticated = true, true
	l.State, l.ProxyRegistration = "CONNECTED", "REGISTERED"
	if fleetHealth(l) != fleetOK {
		t.Fatal("authenticated registered FRP with blocked ICMP must be healthy")
	}
	l.Linked, l.Authenticated = false, false
	if fleetHealth(l) != fleetDeg {
		t.Fatal("unlinked FRP with dead ping stays degraded")
	}
}

func TestDashboardRollupMatchesFleet(t *testing.T) {
	ok := mkLive(1, true, true, false, "")
	ok.Linked = true
	ok.Managed, ok.Authenticated = true, true
	ok.State, ok.ProxyRegistration = "CONNECTED", "REGISTERED"
	bad := mkLive(2, true, true, false, "")
	if fleetHealth(ok) != fleetOK || fleetHealth(bad) != fleetDeg {
		t.Fatal("registered => healthy, unverified+no ping => degraded")
	}
}
