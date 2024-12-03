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

func (bridge *SnapcastMQTTBridge) publishServerStatus(serverStatus SnapcastServer) {

	//todo publish server status?

	for _, client := range serverStatus.Clients {
		bridge.publishClientStatus(client)
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

func (bridge *SnapcastMQTTBridge) processServerStatus() {

	res, err := bridge.SnapClient.Send(context.Background(), snapcast.MethodServerGetStatus, struct{}{})
	if err != nil {
		slog.Error("Error ...", "error", err)
	}
	if res.Error != nil {
		slog.Error("Error ...", "error", res.Error)
	}

	serverStatusRes, err := snapcast.ParseResult[snapcast.ServerGetStatusResponse](res.Result)
	if err != nil {
		slog.Error("Error ...", "error", res.Error)
	}

	serverStatus, err := parseServerStatus(serverStatusRes)
	if err != nil {
		slog.Error("Error ...", "error", err)
	}

	bridge.publishServerStatus(*serverStatus)
	bridge.ServerStatus = *serverStatus
}

func (bridge *SnapcastMQTTBridge) processGroupStatus(groupID string) {

	res, err := bridge.SnapClient.Send(context.Background(), snapcast.MethodGroupGetStatus, &snapcast.GroupGetStatusRequest{ID: groupID})
	if err != nil {
		slog.Error("Error ...", "error", err)
	}
	if res.Error != nil {
		slog.Error("Error ...", "error", res.Error)
	}

	groupStatusRes, err := snapcast.ParseResult[snapcast.GroupGetStatusResponse](res.Result)
	if err != nil {
		slog.Error("Error ...", "error", res.Error)
	}

	groupStatus, err := parseGroupStatus(&groupStatusRes.Group)
	bridge.ServerStatus.Groups[groupStatus.GroupID] = *groupStatus
}

func (bridge *SnapcastMQTTBridge) MainLoop() {

	var notify = &snapclient.Notifications{
		MsgReaderErr:          make(chan error),
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

			case <-notify.ClientOnConnect:
				bridge.processServerStatus()
			case <-notify.ClientOnDisconnect:
				bridge.processServerStatus()
			case <-notify.ClientOnNameChanged:
				bridge.processServerStatus()
			case <-notify.ClientOnVolumeChanged:
				bridge.processServerStatus() //todo update value directly?

			case m := <-notify.GroupOnMute:
				bridge.processGroupStatus(m.ID) //todo update value directly?
			case m := <-notify.GroupOnNameChanged:
				bridge.processGroupStatus(m.ID)
			case m := <-notify.GroupOnStreamChanged:
				bridge.processGroupStatus(m.ID)

			case <-notify.ServerOnUpdate:
				bridge.processServerStatus()
			case m := <-notify.MsgReaderErr:
				slog.Debug("Message reader error", "error", m.Error())
				continue
			}
		}
	}()

	panic(<-wsClose)

}
