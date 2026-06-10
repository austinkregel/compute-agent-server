package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/austinkregel/backup-server/internal/ws"
)

// These handlers expose read-only Docker/Swarm monitoring for connected agents.
// The agent is monitoring-only: it reports container/swarm state but does not
// deploy or manage stacks (that is owned by a separate tool).

func handleClientDocker(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		stats := deps.Store.GetStats(clientID)
		if stats == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no stats for client"})
			return
		}
		docker := map[string]any{
			"clientId":        clientID,
			"dockerAvailable": stats["dockerAvailable"],
			"swarmActive":     stats["swarmActive"],
			"swarmRole":       stats["swarmRole"],
			"containers":      stats["containers"],
		}
		writeJSON(w, http.StatusOK, docker)
	}
}

func handleSwarmClusters(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"clusters": deps.Store.SwarmClusters()})
	}
}

func handleSwarmCluster(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clusterID := chi.URLParam(r, "clusterId")
		clusters := deps.Store.SwarmClusters()
		for _, c := range clusters {
			if cid, _ := c["clusterId"].(string); cid == clusterID {
				writeJSON(w, http.StatusOK, c)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster not found"})
	}
}

// handleContainerInventory asks an agent to enumerate every container on the
// host, classified as managed/swarm/unmanaged.
func handleContainerInventory(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		if !deps.Store.HasClient(clientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}

		token := fmt.Sprintf("container-inv-%s-%d", clientID, time.Now().UnixMilli())
		ch := deps.Relay.RegisterGenericPending(token)
		defer deps.Relay.UnregisterGenericPending(token)

		ws.SendSignedCommand(deps.Store, clientID, "container_inventory",
			map[string]any{"token": token}, deps.Log)

		select {
		case result := <-ch:
			writeJSON(w, http.StatusOK, result)
		case <-time.After(15 * time.Second):
			writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "timeout waiting for container inventory"})
		case <-r.Context().Done():
			return
		}
	}
}

// handleContainerLogs tails logs for a single container on a connected agent.
// Client-scoped (no stack concept) since the agent reports on the host's
// existing containers regardless of who deployed them.
func handleContainerLogs(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		if !deps.Store.HasClient(clientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}
		containerID := chi.URLParam(r, "containerId")
		if containerID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "containerId is required"})
			return
		}

		tail := r.URL.Query().Get("tail")
		if tail == "" {
			tail = "100"
		}

		token := fmt.Sprintf("container-logs-%s-%d", containerID, time.Now().UnixMilli())
		ch := deps.Relay.RegisterGenericPending(token)
		defer deps.Relay.UnregisterGenericPending(token)

		ws.SendSignedCommand(deps.Store, clientID, "container_logs", map[string]any{
			"containerId": containerID,
			"tail":        tail,
			"token":       token,
		}, deps.Log)

		select {
		case result := <-ch:
			writeJSON(w, http.StatusOK, result)
		case <-time.After(15 * time.Second):
			writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "timeout waiting for container logs"})
		case <-r.Context().Done():
			return
		}
	}
}
