package main

import "testing"

func TestLinkedFromSS(t *testing.T) {
	ss := "0 0 10.10.10.1:7091 10.10.10.2:51000\n0 0 5.75.195.15:7093 1.2.3.4:40000\n0 0 [::ffff:77.1.1.1]:22 9.9.9.9:5\n"
	if !linkedFromSS(ss, []int{7091, 7093, 7089}, "5.75.195.15", "10.10.10.2") {
		t.Fatal("GRE-inner session must count")
	}
	if linkedFromSS(ss, []int{7091}, "8.8.8.8", "10.99.0.2") {
		t.Fatal("foreign remote must not match")
	}
	if !linkedFromSS(ss, []int{7093}) {
		t.Fatal("no remote filter must match any client")
	}
	if linkedFromSS("", []int{7091}) {
		t.Fatal("empty ss output is not linked")
	}
}

func TestFleetHealthFrpOnlyIsHealthy(t *testing.T) {
	l := mkLive(1, true, true, false, "")
	l.Linked = true
	if fleetHealth(l) != fleetOK {
		t.Fatal("linked FRP with dead GRE ping must be healthy")
	}
	l.Linked = false
	if fleetHealth(l) != fleetDeg {
		t.Fatal("unlinked FRP with dead ping stays degraded")
	}
}

func TestDashboardRollupMatchesFleet(t *testing.T) {
	ok := mkLive(1, true, true, false, "")
	ok.Linked = true
	bad := mkLive(2, true, true, false, "")
	if fleetHealth(ok) != fleetOK || fleetHealth(bad) != fleetDeg {
		t.Fatal("linked => healthy, unlinked+no ping => degraded")
	}
}
