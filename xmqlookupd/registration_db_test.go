package xmqlookupd

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"mlib.com/xmq/internal/protocol"
	"mlib.com/xmq/internal/test"
	"mlib.com/xmq/pbapi"
)

func TestRegistrationDB(t *testing.T) {
	sec30 := 30 * time.Second
	beginningOfTime := time.Unix(1348797047, 0)
	pi1 := &protocol.PeerInfo{
		PeerInfo: pbapi.PeerInfo{
			Id:               "1",
			RemoteAddress:    "remote_addr:1",
			Hostname:         "host",
			BroadcastAddress: "b_addr",
			TcpPort:          1,
			HttpPort:         2,
			Version:          "v1",
			LastUpdate:       beginningOfTime.UnixNano(),
		},
	}
	pi2 := &protocol.PeerInfo{
		PeerInfo: pbapi.PeerInfo{
			Id:               "2",
			RemoteAddress:    "remote_addr:2",
			Hostname:         "host",
			BroadcastAddress: "b_addr",
			TcpPort:          2,
			HttpPort:         3,
			Version:          "v1",
			LastUpdate:       beginningOfTime.UnixNano(),
		},
	}
	pi3 := &protocol.PeerInfo{
		PeerInfo: pbapi.PeerInfo{
			Id:               "3",
			RemoteAddress:    "remote_addr:3",
			Hostname:         "host",
			BroadcastAddress: "b_addr",
			TcpPort:          3,
			HttpPort:         4,
			Version:          "v1",
			LastUpdate:       beginningOfTime.UnixNano(),
		},
	}
	p1 := &Producer{pi1, false, beginningOfTime}
	p2 := &Producer{pi2, false, beginningOfTime}
	p3 := &Producer{pi3, false, beginningOfTime}
	p4 := &Producer{pi1, false, beginningOfTime}

	db := NewRegistrationDB()

	// add producers
	db.AddProducer(Registration{"c", "a", ""}, p1)
	db.AddProducer(Registration{"c", "a", ""}, p2)
	db.AddProducer(Registration{"c", "a", "b"}, p2)
	db.AddProducer(Registration{"d", "a", ""}, p3)
	db.AddProducer(Registration{"t", "a", ""}, p4)

	// find producers
	r := db.FindRegistrations("c", "*", "").Keys()
	test.Equal(t, 1, len(r))
	test.Equal(t, "a", r[0])

	p := db.FindProducers("t", "*", "")
	t.Logf("%s", p)
	test.Equal(t, 1, len(p))
	p = db.FindProducers("c", "*", "")
	t.Logf("%s", p)
	test.Equal(t, 2, len(p))
	p = db.FindProducers("c", "a", "")
	t.Logf("%s", p)
	test.Equal(t, 2, len(p))
	p = db.FindProducers("c", "*", "b")
	t.Logf("%s", p)
	test.Equal(t, 1, len(p))
	test.Equal(t, p2.peerInfo.Id, p[0].peerInfo.Id)

	// filter by active
	test.Equal(t, 0, len(p.FilterByActive(sec30, sec30)))
	p2.peerInfo.LastUpdate = time.Now().UnixNano()
	test.Equal(t, 1, len(p.FilterByActive(sec30, sec30)))
	p = db.FindProducers("c", "*", "")
	t.Logf("%s", p)
	test.Equal(t, 1, len(p.FilterByActive(sec30, sec30)))

	// tombstoning
	fewSecAgo := time.Now().Add(-5 * time.Second).UnixNano()
	p1.peerInfo.LastUpdate = fewSecAgo
	p2.peerInfo.LastUpdate = fewSecAgo
	test.Equal(t, 2, len(p.FilterByActive(sec30, sec30)))
	p1.Tombstone()
	test.Equal(t, 1, len(p.FilterByActive(sec30, sec30)))
	time.Sleep(10 * time.Millisecond)
	test.Equal(t, 2, len(p.FilterByActive(sec30, 5*time.Millisecond)))
	// make sure we can still retrieve p1 from another registration see #148
	test.Equal(t, 1, len(db.FindProducers("t", "*", "").FilterByActive(sec30, sec30)))

	// keys and subkeys
	k := db.FindRegistrations("c", "b", "").Keys()
	test.Equal(t, 0, len(k))
	k = db.FindRegistrations("c", "a", "").Keys()
	test.Equal(t, 1, len(k))
	test.Equal(t, "a", k[0])
	k = db.FindRegistrations("c", "*", "b").SubKeys()
	test.Equal(t, 1, len(k))
	test.Equal(t, "b", k[0])

	// removing producers
	db.RemoveProducer(Registration{"c", "a", ""}, p1.peerInfo.Id)
	p = db.FindProducers("c", "*", "*")
	t.Logf("%s", p)
	test.Equal(t, 1, len(p))

	db.RemoveProducer(Registration{"c", "a", ""}, p2.peerInfo.Id)
	db.RemoveProducer(Registration{"c", "a", "b"}, p2.peerInfo.Id)
	p = db.FindProducers("c", "*", "*")
	t.Logf("%s", p)
	test.Equal(t, 0, len(p))

	// do some key removals
	k = db.FindRegistrations("c", "*", "*").Keys()
	test.Equal(t, 2, len(k))
	db.RemoveRegistration(Registration{"c", "a", ""})
	db.RemoveRegistration(Registration{"c", "a", "b"})
	k = db.FindRegistrations("c", "*", "*").Keys()
	test.Equal(t, 0, len(k))
}

func fillRegDB(registrations int, producers int) *RegistrationDB {
	regDB := NewRegistrationDB()
	for i := 0; i < registrations; i++ {
		regT := Registration{"topic", "t" + strconv.Itoa(i), ""}
		regCa := Registration{"channel", "t" + strconv.Itoa(i), "ca" + strconv.Itoa(i)}
		regCb := Registration{"channel", "t" + strconv.Itoa(i), "cb" + strconv.Itoa(i)}
		for j := 0; j < producers; j++ {
			p := Producer{
				peerInfo: &protocol.PeerInfo{
					PeerInfo: pbapi.PeerInfo{
						Id: "p" + strconv.Itoa(j),
					},
				},
			}
			regDB.AddProducer(regT, &p)
			regDB.AddProducer(regCa, &p)
			regDB.AddProducer(regCb, &p)
		}
	}
	return regDB
}

func benchmarkLookupRegistrations(b *testing.B, registrations int, producers int) {
	regDB := fillRegDB(registrations, producers)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := strconv.Itoa(rand.Intn(producers))
		_ = regDB.LookupRegistrations("p" + j)
	}
}

func benchmarkRegister(b *testing.B, registrations int, producers int) {
	for i := 0; i < b.N; i++ {
		_ = fillRegDB(registrations, producers)
	}
}

func benchmarkDoLookup(b *testing.B, registrations int, producers int) {
	regDB := fillRegDB(registrations, producers)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topic := "t" + strconv.Itoa(rand.Intn(registrations))
		_ = regDB.FindRegistrations("topic", topic, "")
		_ = regDB.FindRegistrations("channel", topic, "*").SubKeys()
		_ = regDB.FindProducers("topic", topic, "")
	}
}

func BenchmarkLookupRegistrations8x8(b *testing.B) {
	benchmarkLookupRegistrations(b, 8, 8)
}

func BenchmarkLookupRegistrations8x64(b *testing.B) {
	benchmarkLookupRegistrations(b, 8, 64)
}

func BenchmarkLookupRegistrations64x64(b *testing.B) {
	benchmarkLookupRegistrations(b, 64, 64)
}

func BenchmarkLookupRegistrations64x512(b *testing.B) {
	benchmarkLookupRegistrations(b, 64, 512)
}

func BenchmarkLookupRegistrations512x512(b *testing.B) {
	benchmarkLookupRegistrations(b, 512, 512)
}

func BenchmarkLookupRegistrations512x2048(b *testing.B) {
	benchmarkLookupRegistrations(b, 512, 2048)
}

func BenchmarkRegister8x8(b *testing.B) {
	benchmarkRegister(b, 8, 8)
}

func BenchmarkRegister8x64(b *testing.B) {
	benchmarkRegister(b, 8, 64)
}

func BenchmarkRegister64x64(b *testing.B) {
	benchmarkRegister(b, 64, 64)
}

func BenchmarkRegister64x512(b *testing.B) {
	benchmarkRegister(b, 64, 512)
}

func BenchmarkRegister512x512(b *testing.B) {
	benchmarkRegister(b, 512, 512)
}

func BenchmarkRegister512x2048(b *testing.B) {
	benchmarkRegister(b, 512, 2048)
}

func BenchmarkDoLookup8x8(b *testing.B) {
	benchmarkDoLookup(b, 8, 8)
}

func BenchmarkDoLookup8x64(b *testing.B) {
	benchmarkDoLookup(b, 8, 64)
}

func BenchmarkDoLookup64x64(b *testing.B) {
	benchmarkDoLookup(b, 64, 64)
}

func BenchmarkDoLookup64x512(b *testing.B) {
	benchmarkDoLookup(b, 64, 512)
}

func BenchmarkDoLookup512x512(b *testing.B) {
	benchmarkDoLookup(b, 512, 512)
}

func BenchmarkDoLookup512x2048(b *testing.B) {
	benchmarkDoLookup(b, 512, 2048)
}
