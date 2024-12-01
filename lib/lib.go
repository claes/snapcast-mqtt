package lib

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/ConnorsApps/snapcast-go/snapcast"
	"github.com/ConnorsApps/snapcast-go/snapclient"
)

type WifiClient struct {
	MacAddress    string `json:"mac_address"`
	Interface     string `json:"interface"`
	Uptime        string `json:"uptime"`
	LastActivity  string `json:"last_activity"`
	SignalToNoise string `json:"signal_to_noise"`
}

type SnapcastMQTTBridge struct {
	MqttClient       mqtt.Client
	SnapClient       *snapclient.Client
	TopicPrefix      string
	SnapClientConfig SnapClientConfig
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

func (bridge *SnapcastMQTTBridge) printServerStatus() {

	res, err := bridge.SnapClient.Send(context.Background(), snapcast.MethodServerGetStatus, struct{}{})
	if err != nil {
		slog.Error("Error ...", "error", err)
	}
	if res.Error != nil {
		slog.Error("Error ...", "error", res.Error)
	}

	serverStatus, err := snapcast.ParseResult[snapcast.ServerGetStatusResponse](res.Result)
	if err != nil {
		slog.Error("Error ...", "error", res.Error)
	}
	fmt.Println("Initial state", serverStatus)
}

func (bridge *SnapcastMQTTBridge) printGroupStatus() {

	res, err := bridge.SnapClient.Send(context.Background(), snapcast.MethodGroupGetStatus, struct{}{})
	if err != nil {
		slog.Error("Error ...", "error", err)
	}
	if res.Error != nil {
		slog.Error("Error ...", "error", res.Error)
	}

	groupStatus, err := snapcast.ParseResult[snapcast.GroupGetStatusResponse](res.Result)
	if err != nil {
		slog.Error("Error ...", "error", res.Error)
	}
	fmt.Println("Group status", groupStatus)
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

	// Listen for events
	go func() {
		for {
			select {
			case m := <-notify.MsgReaderErr:
				fmt.Println("Message reader error", m)
				continue
			case m := <-notify.GroupOnStreamChanged:
				fmt.Println("GroupOnStreamChanged", m)
			case m := <-notify.ServerOnUpdate:
				fmt.Println("ServerOnUpdate", m)
			case m := <-notify.ClientOnVolumeChanged:
				fmt.Println("ClientOnVolumeChanged", m)
			case m := <-notify.ClientOnNameChanged:
				fmt.Println("ClientOnNameChanged", m)
			}
		}
	}()

	panic(<-wsClose)

	// res, err := bridge.SnapClient.Send(context.Background(), snapcast.MethodServerGetStatus, struct{}{})

	// check(err)
	// if res.Error != nil {
	// 	log.Fatalln(res.Error)
	// }

	// initialState, err := snapcast.ParseResult[snapcast.ServerGetStatusResponse](res.Result)
	// check(err)
	// fmt.Println("Initial state", initialState)

	// for {
	// 	reconnectRouterOsClient := false
	// 	reply, err := bridge.RouterOSClient.Run("/interface/wireless/registration-table/print")
	// 	if err != nil {
	// 		slog.Error("Could not retrieve registration table", "error", err)
	// 		reconnectRouterOsClient = true
	// 	} else {
	// 		var clients []WifiClient
	// 		for _, re := range reply.Re {
	// 			client := WifiClient{
	// 				MacAddress:    re.Map["mac-address"],
	// 				Interface:     re.Map["interface"],
	// 				Uptime:        re.Map["uptime"],
	// 				LastActivity:  re.Map["last-activity"],
	// 				SignalToNoise: re.Map["signal-to-noise"],
	// 			}
	// 			clients = append(clients, client)
	// 		}
	// 		jsonData, err := json.MarshalIndent(clients, "", "    ")
	// 		if err != nil {
	// 			slog.Error("Failed to create json", "error", err)
	// 			continue
	// 		}
	// 		bridge.PublishMQTT("routeros/wificlients", string(jsonData), false)
	// 		bridge.MqttClient.IsConnected()
	// 	}

	// 	time.Sleep(30 * time.Second)
	// 	if reconnectRouterOsClient {
	// 		slog.Error("Reconnecting RouterOS client")
	// 		err = bridge.RouterOSClient.Close()
	// 		if err != nil {
	// 			slog.Error("Error when closing RouterOS client", "error", err)
	// 		}
	// 		client, err := CreateRouterOSClient(bridge.RouterOSClientConfig)
	// 		if err != nil {
	// 			slog.Error("Error when recreating RouterOS client", "error", err)
	// 		}
	// 		bridge.RouterOSClient = client
	// 	}
	// }
}
