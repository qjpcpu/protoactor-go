package cluster

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/qjpcpu/protoactor-go/log"
)

func Benchmark_Rendezvous_Get(b *testing.B) {
	SetLogLevel(log.ErrorLevel)
	for _, v := range []int{1, 2, 3, 5, 10, 100, 1000, 2000} {
		members := _newTopologyEventForTest(v)
		ms := newDefaultMemberStrategy("kind").(*simpleMemberStrategy)
		for _, member := range members {
			ms.AddMember(member)
		}
		obj := NewRendezvous(ms)
		obj.UpdateRdv()
		testName := fmt.Sprintf("member*%d", v)
		runtime.GC()
		b.Run(testName, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				address := obj.GetByRdv("0123456789abcdefghijklmnopqrstuvwxyz")
				if address == "" {
					b.Fatalf("empty address res=%d", len(members))
				}
			}
		})
	}
}
