package network

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	libp2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
)

// discoveryLog is a package-level logger for mDNS discovery.
var discoveryLog = logging.New("discovery")

const (
	// MaxDiscoveredPeers is the upper bound on mDNS-discovered peers kept in
	// memory. Beyond this limit new peers are ignored until existing ones
	// are removed.
	MaxDiscoveredPeers = 200

	// BackoffCapSeconds is the maximum backoff delay (in seconds) after
	// repeated connection failures to a peer.
	BackoffCapSeconds = 300 // 5 minutes

	// How often to ping known peers to detect stale connections.
	peerHealthCheckInterval = 30 * time.Second

	// Timeout for individual peer pings.
	peerPingTimeout = 5 * time.Second

	// Delay before attempting to restart mDNS after a failure.
	mdnsRestartDelay = 10 * time.Second
)

// mdnsDiscovery handles mDNS peer discovery events.
type mdnsDiscovery struct {
	host    host.Host
	peers   []peer.AddrInfo
	mu      sync.RWMutex
	ctx     context.Context
	backoff map[peer.ID]*backoffState
	mdnsSvc mdns.Service
	cancel  context.CancelFunc
}

// backoffState tracks exponential backoff for failed peer connections.
type backoffState struct {
	failures int
	nextTry  time.Time
}

// HandlePeerFound is called when a new peer is discovered via mDNS.
func (d *mdnsDiscovery) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == d.host.ID() {
		return // skip self
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if we already know this peer
	for _, existing := range d.peers {
		if existing.ID == pi.ID {
			return // already known
		}
	}

	// Don't add more peers if at the limit
	if len(d.peers) >= MaxDiscoveredPeers {
		discoveryLog.Warn("max peers reached, ignoring new peer", logging.Int("limit", MaxDiscoveredPeers), logging.String("peer", pi.ID.String()[:12]))
		return
	}

	discoveryLog.Info("found peer", logging.String("peer", pi.ID.String()[:12]))

	// Check backoff before connecting
	if d.backoff == nil {
		d.backoff = make(map[peer.ID]*backoffState)
	}

	if bs, ok := d.backoff[pi.ID]; ok {
		if time.Now().Before(bs.nextTry) {
			discoveryLog.Debug("peer in backoff", logging.String("peer", pi.ID.String()[:12]), logging.String("until", bs.nextTry.Format(time.RFC3339)))
			return
		}
	}

	// Auto-connect to discovered peers
	if err := d.host.Connect(d.ctx, pi); err != nil {
		discoveryLog.Warn("connect failed", logging.String("peer", pi.ID.String()[:12]), logging.Err(err))
		d.applyBackoff(pi.ID)
		return
	}

	discoveryLog.Info("connected to peer", logging.String("peer", pi.ID.String()[:12]))
	d.peers = append(d.peers, pi)

	// Clear backoff on successful connection
	delete(d.backoff, pi.ID)
}

// applyBackoff increases the backoff duration for a peer after a failed connection.
// Must be called with d.mu held.
func (d *mdnsDiscovery) applyBackoff(id peer.ID) {
	if d.backoff == nil {
		d.backoff = make(map[peer.ID]*backoffState)
	}

	bs, ok := d.backoff[id]
	if !ok {
		bs = &backoffState{}
		d.backoff[id] = bs
	}

	bs.failures++
	// Exponential backoff: 2^failures seconds, capped at 5 minutes
	delaySec := math.Min(math.Pow(2, float64(bs.failures)), BackoffCapSeconds)
	bs.nextTry = time.Now().Add(time.Duration(delaySec) * time.Second)
}

// RemovePeer removes a peer from the discovered peers list.
func (d *mdnsDiscovery) RemovePeer(id peer.ID) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, p := range d.peers {
		if p.ID == id {
			d.peers = append(d.peers[:i], d.peers[i+1:]...)
			return
		}
	}
}

// Close stops the mDNS service and clears all internal state (peers, backoff).
func (d *mdnsDiscovery) Close() {
	if d.mdnsSvc != nil {
		if err := d.mdnsSvc.Close(); err != nil {
			discoveryLog.Warn("close mDNS service failed", logging.Err(err))
		}
	}

	d.mu.Lock()
	d.peers = nil
	d.backoff = nil
	d.mu.Unlock()

	discoveryLog.Info("mDNS discovery closed")
}

// Peers returns all discovered peers.
func (d *mdnsDiscovery) Peers() []peer.AddrInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]peer.AddrInfo, len(d.peers))
	copy(result, d.peers)
	return result
}

// StartHealthCheck launches a background goroutine that periodically pings
// known peers to detect stale connections. Disconnected peers are removed
// from the list so mDNS can rediscover and reconnect to them.
func (d *mdnsDiscovery) StartHealthCheck(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(peerHealthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.checkPeerHealth()
			}
		}
	}()
	discoveryLog.Info("peer health check started")
}

// checkPeerHealth pings all known peers and removes those that are no longer
// reachable.
func (d *mdnsDiscovery) checkPeerHealth() {
	d.mu.RLock()
	peersCopy := make([]peer.AddrInfo, len(d.peers))
	copy(peersCopy, d.peers)
	d.mu.RUnlock()

	for _, pi := range peersCopy {
		// Use connectedness as a quick check before pinging.
		if d.host.Network().Connectedness(pi.ID) != libp2pnet.Connected {
			discoveryLog.Info("peer no longer connected, removing", logging.String("peer", pi.ID.String()[:12]))
			d.RemovePeer(pi.ID)
			continue
		}

		// Attempt a lightweight ping via opening a stream with a short timeout.
		pingCtx, pingCancel := context.WithTimeout(d.ctx, peerPingTimeout)
		s, err := d.host.NewStream(pingCtx, pi.ID, "/ipfs/ping/1.0.0")
		pingCancel()
		if err != nil {
			discoveryLog.Info("peer ping failed, removing", logging.String("peer", pi.ID.String()[:12]), logging.Err(err))
			d.RemovePeer(pi.ID)
			continue
		}
		s.Close()
	}
}

// RestartMdns attempts to restart the mDNS service if it has failed. This
// should be called when mDNS discovery stops working. It closes the old
// service (if possible) and creates a new one.
func (d *mdnsDiscovery) RestartMdns() error {
	// Close existing service if set.
	if d.mdnsSvc != nil {
		if err := d.mdnsSvc.Close(); err != nil {
			discoveryLog.Warn("close old mDNS service failed", logging.Err(err))
		}
	}

	svc := mdns.NewMdnsService(d.host, "aip-network", d)
	if err := svc.Start(); err != nil {
		return fmt.Errorf("restart mDNS: %w", err)
	}
	d.mdnsSvc = svc
	discoveryLog.Info("mDNS service restarted")
	return nil
}

// StartMdnsWatchdog monitors the mDNS service and restarts it if peer
// discovery appears to have stalled (no peers found for a long time while
// peers are expected on the local network). This runs until ctx is done.
func (d *mdnsDiscovery) StartMdnsWatchdog(ctx context.Context) {
	go func() {
		// Check every 2 minutes whether mDNS is still discovering.
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		lastPeerCount := 0
		staleCycles := 0

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentCount := len(d.Peers())
				if currentCount == 0 && lastPeerCount == 0 {
					staleCycles++
				} else {
					staleCycles = 0
				}
				lastPeerCount = currentCount

				// If we've seen no peers for 3 consecutive checks (6 min),
				// assume mDNS is broken and restart.
				if staleCycles >= 3 {
					discoveryLog.Warn("mDNS appears stale, restarting service")
					time.Sleep(mdnsRestartDelay)
					if err := d.RestartMdns(); err != nil {
						discoveryLog.Error("mDNS restart failed", logging.Err(err))
					}
					staleCycles = 0
				}
			}
		}
	}()
	discoveryLog.Info("mDNS watchdog started")
}
