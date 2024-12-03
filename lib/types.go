package lib

import (
	"log/slog"

	"github.com/ConnorsApps/snapcast-go/snapcast"
)

type SnapcastServer struct {
	Groups  map[string]SnapcastGroup  `json:"groups"`
	Streams map[string]SnapcastStream `json:"streams"`
	Clients map[string]SnapcastClient `json:"clients"`
}

// snapcast/client/id
// Client.GetStatus
type SnapcastClient struct {
	ClientID  string  `json:"client_id"`
	Host      string  `json:"host"`
	GroupID   string  `json:"group_id"`
	GroupName string  `json:"group_name"`
	StreamID  string  `json:"stream_id"`
	Connected bool    `json:"connected"` // true, false
	Volume    float64 `json:"volume"`
	Muted     bool    `json:"muted"` // true, false
}

// snapcast/stream/id
type SnapcastStream struct {
	StreamID string `json:"stream_id"`
	Status   string `json:"status"` // playing, idle
}

// snapcast/group/id
// Group.GetStatus
type SnapcastGroup struct {
	GroupID   string                    `json:"group_id"`
	GroupName string                    `json:"group_name"`
	Muted     bool                      `json:"muted"`
	StreamID  string                    `json:"stream_id"`
	Clients   map[string]SnapcastClient `json:"clients"`
}

func parseClientStatus(c *snapcast.Client, g *snapcast.Group) (*SnapcastClient, error) {
	client := &SnapcastClient{
		ClientID:  c.ID,
		Host:      c.Host.Name,
		Connected: c.Connected,
		GroupID:   g.ID,
		GroupName: g.Name,
		StreamID:  g.StreamID,
		Muted:     c.Config.Volume.Muted,
		Volume:    float64(c.Config.Volume.Percent),
	}
	return client, nil
}

func parseGroupStatus(g *snapcast.Group) (*SnapcastGroup, error) {

	group := &SnapcastGroup{
		GroupID:   g.ID,
		GroupName: g.Name,
		Muted:     g.Muted,
		StreamID:  g.StreamID,
	}

	clients := make(map[string]SnapcastClient)
	for _, c := range g.Clients {
		client, err := parseClientStatus(&c, g)
		if err != nil {
			slog.Error("Error parsing client status", "error", err)
			return nil, err
		}
		clients[client.ClientID] = *client
	}
	group.Clients = clients
	return group, nil
}

func parseServerStatus(data *snapcast.ServerGetStatusResponse) (*SnapcastServer, error) {

	allClients := make(map[string]SnapcastClient)
	groups := make(map[string]SnapcastGroup)
	for _, g := range data.Server.Groups {
		group, err := parseGroupStatus(&g)
		if err != nil {
			slog.Error("Error parsing group status", "error", err)
			return nil, err
		}
		groups[group.GroupID] = *group
		for clientID, client := range group.Clients {
			allClients[clientID] = client
		}
	}

	streams := make(map[string]SnapcastStream)
	for _, stream := range data.Server.Streams {
		streams[stream.ID] = SnapcastStream{
			StreamID: stream.ID,
			Status:   string(stream.Status),
		}
	}

	return &SnapcastServer{Groups: groups, Streams: streams, Clients: allClients}, nil
}
