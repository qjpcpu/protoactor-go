package cluster

import "github.com/qjpcpu/protoactor-go/actor"

type IdentityLookup interface {
	Get(clusterIdentity *ClusterIdentity)
	RemovePid(clusterIdentity *ClusterIdentity, pid *actor.PID)
	Setup(cluster *Cluster, kinds []string, isClient bool)
	Shutdown()
}
