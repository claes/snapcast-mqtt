package lib

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/ConnorsApps/snapcast-go/snapcast"
	"github.com/ConnorsApps/snapcast-go/snapclient"
)

type SnapcastMQTTBridge struct {
	MqttClient       mqtt.Client
	SnapClient       *snapclient.Client
	TopicPrefix      string
	SnapClientConfig SnapClientConfig
	ServerStatus     SnapcastServer
}

type SnapClientConfig struct {
	SnapServerAddress string
}

type MQTTClientConfig struct {
	MQTTBroker string
}

func CreateSnapclient(config SnapClientConfig) (*snapclient.Client, error) {
	var client = snapclient.New(&snapclient.Options{
		Host:             config.SnapServerAddress,
		SecureConnection: false,
	})
	return client, nil
}

func CreateMQTTClient(mqttBroker string) (mqtt.Client, error) {
	slog.Info("Creating MQTT client", "broker", mqttBroker)
	opts := mqtt.NewClientOptions().AddBroker(mqttBroker).SetAutoReconnect(true)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		slog.Error("Could not connect to broker", "mqttBroker", mqttBroker, "error", token.Error())
		return nil, token.Error()
	}
	slog.Info("Connected to MQTT broker", "mqttBroker", mqttBroker)

	return client, nil
}

func NewSnapcastMQTTBridge(snapClientConfig SnapClientConfig, mqttClient mqtt.Client, topicPrefix string) (*SnapcastMQTTBridge, error) {

	snapClient, err := CreateSnapclient(snapClientConfig)
	if err != nil {
		slog.Error("Error creating Snapcast client", "error", err, "address", snapClientConfig.SnapServerAddress)
		return nil, err
	}

	bridge := &SnapcastMQTTBridge{
		MqttClient:       mqttClient,
		SnapClient:       snapClient,
		TopicPrefix:      topicPrefix,
		SnapClientConfig: snapClientConfig,
	}

	return bridge, nil
}

func prefixify(topicPrefix, subtopic string) string {
	if len(strings.TrimSpace(topicPrefix)) > 0 {
		return topicPrefix + "/" + subtopic
	} else {
		return subtopic
	}
}

func (bridge *SnapcastMQTTBridge) PublishMQTT(subtopic string, message string, retained bool) {
	token := bridge.MqttClient.Publish(prefixify(bridge.TopicPrefix, subtopic), 0, retained, message)
	token.Wait()
}

func (bridge *SnapcastMQTTBridge) publishServerStatus(serverStatus SnapcastServer, publishGroup, publishClient, publishStream bool) {

	// TODO: Delete what no longer exist?

	if publishGroup {
		for _, group := range serverStatus.Groups {
			bridge.publishGroupStatus(group)
		}
	}
	if publishClient {
		for _, client := range serverStatus.Clients {
			bridge.publishClientStatus(client)
		}
	}
	if publishStream {
		for _, stream := range serverStatus.Streams {
			bridge.publishStreamStatus(stream)
		}
	}
}

func (bridge *SnapcastMQTTBridge) publishStreamStatus(streamStatus SnapcastStream) {
	publish := false
	if bridge.ServerStatus.Clients != nil {
		currentStreamStatus, exists := bridge.ServerStatus.Streams[streamStatus.StreamID]
		publish = !(exists && currentStreamStatus == streamStatus)
	} else {
		publish = true
	}

	if publish {
		jsonData, err := json.MarshalIndent(streamStatus, "", "    ")
		if err != nil {
			slog.Error("Failed to create json for stream", "error", err, "stream", streamStatus)
			return
		}
		bridge.PublishMQTT("snapcast/stream/"+streamStatus.StreamID, string(jsonData), true)
	}
}

func (bridge *SnapcastMQTTBridge) publishClientStatus(clientStatus SnapcastClient) {
	publish := false
	if bridge.ServerStatus.Clients != nil {
		currentClientStatus, exists := bridge.ServerStatus.Clients[clientStatus.ClientID]
		publish = !(exists && currentClientStatus == clientStatus)
	} else {
		publish = true
	}

	if publish {
		jsonData, err := json.MarshalIndent(clientStatus, "", "    ")
		if err != nil {
			slog.Error("Failed to create json for client", "error", err, "client", clientStatus.ClientID)
			return
		}
		bridge.PublishMQTT("snapcast/client/"+clientStatus.ClientID, string(jsonData), true)
	}
}

func (bridge *SnapcastMQTTBridge) publishGroupStatus(groupStatus SnapcastGroup) {
	publish := false
	if bridge.ServerStatus.Groups != nil {
		currentGroupStatus, exists := bridge.ServerStatus.Groups[groupStatus.GroupID]
		publish = !(exists && snapcastGroupsEqual(currentGroupStatus, groupStatus))
	} else {
		publish = true
	}

	if publish {
		jsonData, err := json.MarshalIndent(groupStatus, "", "    ")
		if err != nil {
			slog.Error("Failed to create json for group", "error", err, "group", groupStatus.GroupID)
			return
		}
		bridge.PublishMQTT("snapcast/group/"+groupStatus.GroupID, string(jsonData), true)
	}
}

func (bridge *SnapcastMQTTBridge) processServerStatus(publishGroup, publishClient, publishStream bool) {

	res, err := bridge.SnapClient.Send(context.Background(), snapcast.MethodServerGetStatus, struct{}{})
	if err != nil {
		slog.Error("Error when requesting server status", "error", err)
	}
	if res.Error != nil {
		slog.Error("Error in response to server get status", "error", res.Error)
	}

	serverStatusRes, err := snapcast.ParseResult[snapcast.ServerGetStatusResponse](res.Result)
	if err != nil {
		slog.Error("Error when parsing server status response", "error", res.Error)
	}

	serverStatus, err := parseServerStatus(serverStatusRes)
	if err != nil {
		slog.Error("Error when parsing server status ", "error", err)
	}

	bridge.publishServerStatus(*serverStatus, publishGroup, publishClient, publishStream)
	bridge.ServerStatus = *serverStatus
}

func (bridge *SnapcastMQTTBridge) processGroupStatus(groupID string) {

	res, err := bridge.SnapClient.Send(context.Background(), snapcast.MethodGroupGetStatus, &snapcast.GroupGetStatusRequest{ID: groupID})
	if err != nil {
		slog.Error("Error when requesting group status", "error", err)
	}
	if res.Error != nil {
		slog.Error("Error in response to group get status", "error", res.Error)
	}

	groupStatusRes, err := snapcast.ParseResult[snapcast.GroupGetStatusResponse](res.Result)
	if err != nil {
		slog.Error("Error when parsing group status response", "error", res.Error)
	}

	groupStatus, err := parseGroupStatus(&groupStatusRes.Group)
	if err != nil {
		slog.Error("Error when parsing group status ", "error", err)
	}

	bridge.publishGroupStatus(*groupStatus)
	bridge.ServerStatus.Groups[groupStatus.GroupID] = *groupStatus
}

func (bridge *SnapcastMQTTBridge) MainLoop() {

	var notify = &snapclient.Notifications{
		MsgReaderErr:          make(chan error),
		StreamOnUpdate:        make(chan *snapcast.StreamOnUpdate),
		ServerOnUpdate:        make(chan *snapcast.ServerOnUpdate),
		GroupOnMute:           make(chan *snapcast.GroupOnMute),
		GroupOnNameChanged:    make(chan *snapcast.GroupOnNameChanged),
		GroupOnStreamChanged:  make(chan *snapcast.GroupOnStreamChanged),
		ClientOnVolumeChanged: make(chan *snapcast.ClientOnVolumeChanged),
		ClientOnNameChanged:   make(chan *snapcast.ClientOnNameChanged),
		ClientOnConnect:       make(chan *snapcast.ClientOnConnect),
		ClientOnDisconnect:    make(chan *snapcast.ClientOnDisconnect),
	}

	wsClose, err := bridge.SnapClient.Listen(notify)
	if err != nil {
		slog.Error("Error listening for notifications on snapclient", "error", err)
	}

	go func() {
		for {
			select {

			case <-notify.StreamOnUpdate:
				bridge.processServerStatus(false, false, true)

			case <-notify.ClientOnConnect:
				bridge.processServerStatus(false, true, false)
			case <-notify.ClientOnDisconnect:
				bridge.processServerStatus(false, true, false)
			case <-notify.ClientOnNameChanged:
				bridge.processServerStatus(false, true, false)
			case <-notify.ClientOnVolumeChanged:
				bridge.processServerStatus(false, true, false) //todo update value directly?

			case m := <-notify.GroupOnMute:
				bridge.processGroupStatus(m.ID) //todo update value directly?
			case m := <-notify.GroupOnNameChanged:
				bridge.processGroupStatus(m.ID)
			case m := <-notify.GroupOnStreamChanged:
				bridge.processGroupStatus(m.ID)

			case <-notify.ServerOnUpdate:
				bridge.processServerStatus(true, true, true)
			case m := <-notify.MsgReaderErr:
				slog.Debug("Message reader error", "error", m.Error())
				continue
			}
		}
	}()
	bridge.processServerStatus(true, true, true)

	panic(<-wsClose)
}
