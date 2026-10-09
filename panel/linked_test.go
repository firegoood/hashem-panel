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

const ssSample = "0      0      10.10.10.1:7091   10.10.10.2:51000\n\t bbr wscale:9,9 rto:275 rtt:74.102/0.085 ato:40 mss:1328\n" +
	"0      0      5.75.195.15:7093   1.2.3.4:40000\n\t cubic rto:201 rtt:0.564/0.087 mss:1\n" +
	"0      0      5.75.195.15:22   9.9.9.9:5\n\t rtt:1/1\n" +
	"0      0      5.75.195.15:7091   10.10.10.2:51001\n"

func TestParseSSRTT(t *testing.T) {
	all := parseSS(ssSample)
	if len(all) != 4 {
		t.Fatalf("want 4 sessions, got %d", len(all))
	}
	if all[0].RTTms != 74.102 || all[1].RTTms != 0.564 || all[3].RTTms != -1 {
		t.Fatalf("rtt parse wrong: %+v", all)
	}
	got := sessionsOn(all, []int{7091}, "10.10.10.2")
	if len(got) != 2 || minRTT(got) != 74.102 {
		t.Fatalf("filter/min wrong: %+v", got)
	}
	if len(sessionsOn(all, []int{7091}, "8.8.8.8")) != 0 {
		t.Fatal("foreign remote must not match")
	}
	if minRTT(nil) != -1 {
		t.Fatal("empty -> -1")
	}
}

func TestPickLatency(t *testing.T) {
	if v, k := pickLatency(true, "12ms", 80); v != 12 || k != "icmp" {
		t.Fatalf("icmp must win: %v %v", v, k)
	}
	if v, k := pickLatency(false, "", 80.04); v != 80 || k != "tcp" {
		t.Fatalf("tcp fallback: %v %v", v, k)
	}
	if v, k := pickLatency(false, "", -1); v != -1 || k != "" {
		t.Fatalf("none: %v %v", v, k)
	}
}

func TestHubLatency(t *testing.T) {
	n := []fleetNode{
		{LatencyMs: 10, LatKind: "icmp"}, {LatencyMs: 90, LatKind: "tcp"},
		{LatencyMs: -1}, {LatencyMs: 20, LatKind: "icmp"},
	}
	h := hubLatency(n)
	if h.Count != 3 || h.Min != 10 || h.Max != 90 || h.Avg != 40 || h.ICMP != 2 || h.TCP != 1 {
		t.Fatalf("hub latency wrong: %+v", h)
	}
	if z := hubLatency(nil); z.Count != 0 || z.Avg != 0 {
		t.Fatalf("empty: %+v", z)
	}
}
