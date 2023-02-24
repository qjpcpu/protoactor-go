package cluster

import (
	"sync"

	"github.com/qjpcpu/protoactor-go/actor"
	"github.com/qjpcpu/protoactor-go/eventstream"
	"github.com/qjpcpu/protoactor-go/log"
)

const (
	ActorNameIdentity  = "partition-identity"
	ActorNamePlacement = "partition-activator"
)

type PartitionManager struct {
	cluster       *Cluster
	kinds         sync.Map
	topologySub   *eventstream.Subscription
	deadletterSub *eventstream.Subscription
	mux           sync.Mutex
}

func newPartitionManager(c *Cluster, kinds ...Kind) *PartitionManager {
	return &PartitionManager{
		cluster: c,
	}
}

// Start ...
func (pm *PartitionManager) Start() {
	system := pm.cluster.ActorSystem
	pm.topologySub = system.EventStream.
		Subscribe(func(ev interface{}) {
			pm.onClusterTopology(ev.(*ClusterTopologyEventV2))
		}).
		WithPredicate(func(m interface{}) bool {
			_, ok := m.(*ClusterTopologyEventV2)
			return ok
		})
}

// Stop ...
func (pm *PartitionManager) Stop() {
	system := pm.cluster.ActorSystem
	system.EventStream.Unsubscribe(pm.topologySub)
	pm.kinds.Range(func(k, v interface{}) bool {
		kind := k.(string)
		pk := v.(*PartitionKind)
		plog.Info("Stopping partition", log.String("kind", kind), log.String("pk", pk.actorNames.Identity))
		pk.stop()
		return true
	})
	plog.Info("Stopped PartitionManager")
}

// PidOfIdentityActor ...
func (pm *PartitionManager) PidOfIdentityActor(kind, addr string) *actor.PID {
	v, ok := pm.kinds.Load(kind)
	if !ok {
		return nil
	}
	pk := v.(*PartitionKind)
	return pk.PidOfIdentityActor(addr)
}

// // PidOfPlacementActor ...
// func (pm *PartitionManager) PidOfPlacementActor(kind, addr string) *actor.PID {
// 	return &actor.PID{Address: addr, Id: ActorNamePlacement}
// }

func (pm *PartitionManager) onClusterTopology(tplg *ClusterTopologyEventV2) {
	pm.mux.Lock()
	defer pm.mux.Unlock()

	_log := plog.With(log.Uint64("eventId", tplg.EventId))
	_log.Debug("onClusterTopology start")

	system := pm.cluster.ActorSystem
	kindGroups := pm.groupClusterTopologyByKind(tplg.ClusterTopology)
	for kind, msg := range kindGroups {
		if v, ok := pm.kinds.Load(kind); ok {
			pk := v.(*PartitionKind)
			system.Root.Send(pk.identity.PID(), msg)
			system.Root.Send(pk.activator.PID(), msg)
		} else {
			// start partition of kind
			chash, _ := tplg.chashByKind[kind]
			pk := newPartitionKind(pm.cluster, kind)
			if err := pk.start(chash); err != nil {
				_log.Error("Start PartitionKind failed", log.String("kind", kind), log.Error(err))
			} else {
				pm.kinds.Store(kind, pk)
				system.Root.Send(pk.identity.PID(), msg)
				system.Root.Send(pk.activator.PID(), msg)
			}
		}
	}

	pm.kinds.Range(func(k, v interface{}) bool {
		kind := k.(string)
		if _, ok := kindGroups[kind]; !ok {
			pk := v.(*PartitionKind)
			pm.kinds.Delete(kind)
			pk.stop()
			_log.Info("Stopped PartitionKind", log.String("kind", kind))
		}
		return true
	})
	_log.Debug("onClusterTopology end")
}

func (pm *PartitionManager) groupClusterTopologyByKind(tplg *ClusterTopology) map[string]*ClusterTopology {
	groups := map[string]*ClusterTopology{}
	for kind, members := range groupMembersByKind(tplg.Members) {
		groups[kind] = &ClusterTopology{Members: members, EventId: tplg.EventId}
	}
	return groups
}

func (pm *PartitionManager) onDeadLetterEvent(ev *actor.DeadLetterEvent) {
	return
	// if ev.Sender == nil {
	// 	return
	// }
	// switch msg := ev.Message.(type) {
	// case *GrainRequest:
	// 	_ = msg
	// 	system := pm.cluster.ActorSystem
	// 	system.Root.Send(ev.Sender, &GrainErrorResponse{Err: "DeadLetter"})
	// }
}
