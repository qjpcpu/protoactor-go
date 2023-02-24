package consul

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/qjpcpu/protoactor-go/actor"

	"github.com/qjpcpu/protoactor-go/cluster"
	"github.com/qjpcpu/protoactor-go/log"
	"github.com/hashicorp/consul/api"
)

var (
	ProviderShuttingDownError = fmt.Errorf("consul cluster provider is shutting down")
)

type Provider struct {
	cluster             *cluster.Cluster
	deregistered        bool
	shutdown            bool
	id                  string
	clusterName         string
	address             string
	port                int
	knownKinds          []string
	index               uint64 // consul blocking index
	client              *api.Client
	ttl                 time.Duration
	refreshTTL          time.Duration
	updateTTLWaitGroup  sync.WaitGroup
	deregisterCritical  time.Duration
	blockingWaitTime    time.Duration
	clusterError        error
	consulServerAddress string
	pid                 *actor.PID
}

func New(opts ...Option) (*Provider, error) {
	return NewWithConfig(&api.Config{}, opts...)
}

func NewWithConfig(consulConfig *api.Config, opts ...Option) (*Provider, error) {
	client, err := api.NewClient(consulConfig)
	if err != nil {
		return nil, err
	}
	p := &Provider{
		client:              client,
		ttl:                 3 * time.Second,
		refreshTTL:          1 * time.Second,
		deregisterCritical:  60 * time.Second,
		blockingWaitTime:    20 * time.Second,
		consulServerAddress: consulConfig.Address,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

func (p *Provider) init(c *cluster.Cluster) error {
	knownKinds := c.GetClusterKinds()
	clusterName := c.Config.Name

	host, port, err := c.ActorSystem.GetHostPort()
	if err != nil {
		return err
	}

	p.cluster = c
	p.id = fmt.Sprintf("%v@%v:%v", clusterName, host, port)
	p.clusterName = clusterName
	p.address = host
	p.port = port
	p.knownKinds = knownKinds
	return nil
}

func (p *Provider) StartMember(c *cluster.Cluster) error {
	err := p.init(c)
	if err != nil {
		return err
	}

	p.pid, err = c.ActorSystem.Root.SpawnNamed(actor.PropsFromProducer(func() actor.Actor {
		return newProviderActor(p)
	}), "consul-provider")
	if err != nil {
		plog.Error("Failed to start consul-provider actor", log.Error(err))
		return err
	}

	return nil
}

func (p *Provider) StartClient(c *cluster.Cluster) error {
	if err := p.init(c); err != nil {
		return err
	}
	p.blockingStatusChange()
	p.monitorMemberStatusChanges()
	return nil
}

func (p *Provider) DeregisterMember() error {
	err := p.deregisterService()
	if err != nil {
		fmt.Println(err)
		return err
	}
	p.deregistered = true
	return nil
}

func (p *Provider) Shutdown(graceful bool) error {
	if p.shutdown {
		return nil
	}
	p.shutdown = true
	if p.pid != nil {
		if err := p.cluster.ActorSystem.Root.StopFuture(p.pid).Wait(); err != nil {
			plog.Error("Failed to stop consul-provider actor", log.Error(err))
		}
		p.pid = nil
	}

	return nil
}

func (p *Provider) UpdateClusterState(state cluster.ClusterState) error {
	if p.shutdown {
		// don't re-register when already in the process of shutting down
		return ProviderShuttingDownError
	}
	value, err := json.Marshal(state.BannedMembers)
	if err != nil {
		plog.Error("Failed to UpdateClusterState. json.Marshal", log.Error(err))
		return err
	}
	kv := &api.KVPair{
		Key:   fmt.Sprintf("%s/banned", p.clusterName),
		Value: value,
	}
	if _, err := p.client.KV().Put(kv, nil); err != nil {
		plog.Error("Failed to UpdateClusterState.", log.Error(err))
		return err
	}
	if err := p.registerService(); err != nil {
		plog.Error("Failed to registerService.", log.Error(err))
		return err
	}
	return nil
}

func blockingUpdateTTL(p *Provider) error {
	p.clusterError = p.client.Agent().UpdateTTL("service:"+p.id, "", api.HealthPassing)
	return p.clusterError
}

func (p *Provider) registerService() error {
	s := &api.AgentServiceRegistration{
		ID:      p.id,
		Name:    p.clusterName,
		Tags:    p.knownKinds,
		Address: p.address,
		Port:    p.port,
		Meta: map[string]string{
			"id": p.id,
		},
		Check: &api.AgentServiceCheck{
			DeregisterCriticalServiceAfter: p.deregisterCritical.String(),
			TTL:                            p.ttl.String(),
		},
	}
	return p.client.Agent().ServiceRegister(s)
}

func (p *Provider) deregisterService() error {
	return p.client.Agent().ServiceDeregister(p.id)
}

// call this directly after registering the service
func (p *Provider) blockingStatusChange() {
	p.notifyStatuses()
}

func (p *Provider) notifyStatuses() {
	statuses, meta, err := p.client.Health().Service(p.clusterName, "", false, &api.QueryOptions{
		WaitIndex: p.index,
		WaitTime:  p.blockingWaitTime,
	})
	if err != nil {
		plog.Error("notifyStatues", log.Error(err))
		return
	}
	p.index = meta.LastIndex

	var members []*cluster.Member
	for _, v := range statuses {
		if len(v.Checks) > 0 && v.Checks.AggregatedStatus() == api.HealthPassing {
			memberId := v.Service.Meta["id"]
			if memberId == "" {
				memberId = fmt.Sprintf("%v@%v:%v", p.clusterName, v.Service.Address, v.Service.Port)
				plog.Info("meta['id'] was empty, fixeds", log.String("id", memberId))
			}
			members = append(members, &cluster.Member{
				Id:    memberId,
				Host:  v.Service.Address,
				Port:  int32(v.Service.Port),
				Kinds: v.Service.Tags,
			})
		}
	}
	// the reason why we want this in a batch and not as individual messages is that
	// if we have an atomic batch, we can calculate what nodes have left the cluster
	// passing events one by one, we can't know if someone left or just haven't changed status for a long time

	// publish the current cluster topology onto the event stream
	p.cluster.MemberList.UpdateClusterTopology(members, meta.LastIndex)
	// res := cluster.TopologyEvent(members)
	// p.cluster.ActorSystem.EventStream.Publish(res)
}

func (p *Provider) monitorMemberStatusChanges() {
	go func() {
		for !p.shutdown {
			p.notifyStatuses()
		}
	}()
}
