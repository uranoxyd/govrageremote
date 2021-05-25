// Copyright 2021 David Ewelt <uranoxyd@gmail.com>
//   This program is free software; you can redistribute it and/or modify
//   it under the terms of the GNU General Public License as published by
//   the Free Software Foundation; either version 3 of the License, or
//   (at your option) any later version.
//
//   This program is distributed in the hope that it will be useful, but
//   WITHOUT ANY WARRANTY; without even the implied warranty of
//   MERCHANTIBILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
//   General Public License for more details.
//
//   You should have received a copy of the GNU General Public License
//   along with this program. If not, see <http://www.gnu.org/licenses/>.

package govrageremote

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"
)

var requestMutex sync.Mutex

//-- https://stackoverflow.com/questions/33144967/what-is-the-c-sharp-datetimeoffset-equivalent-in-go/33161703#33161703
//-- This feels just not right, I'm looking in your direction Keen Software House :)
func timeFromTicks(ticks int64) time.Time {
	return time.Unix(ticks/10e6-62135596800, ticks%10e6)
}
func ticksFromTime(value time.Time) int64 {
	return int64(((float64(value.UnixNano()) / 1e9) + 62135596800) * 10e6)
}

type VRagePositionable interface {
	GetPosition() VRagePosition
}

type VRageRemoteClient struct {
	BaseURL       string
	RemoteAddress string
	Key           string
	httpClient    *http.Client
	nonce         int64
}

type VRagePosition struct {
	X float64
	Y float64
	Z float64
}

func (pos VRagePosition) DistanceTo(other VRagePosition) float64 {
	x := other.X - pos.X
	y := other.Y - pos.Y
	z := other.Z - pos.Z
	return math.Sqrt(x*x + y*y + z*z)
}

type VRageRemoteResponse struct {
	Error *VRageRemoteResponseError `json:"error,omitempty"`
	Meta  *VRageRemoteResponseMeta  `json:"meta"`
}
type VRageRemoteResponseMeta struct {
	ApiVersion string  `json:"apiVersion"`
	QueryTime  float64 `json:"queryTime"`
}
type VRageRemoteResponseError struct {
	Message string `json:"message"`
}

//--
//-- Characters
//--

type VRageRemoteCharacterListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteCharacterList `json:"data"`
}
type VRageRemoteCharacterList struct {
	Characters []*VRageRemoteCharacter
}
type VRageRemoteCharacter struct {
	client      *VRageRemoteClient
	DisplayName string
	EntityID    int64 `json:"EntityId"`
	Mass        float64
	Position    VRagePosition
	LinearSpeed float64
}

func (char *VRageRemoteCharacter) GetPosition() VRagePosition {
	return char.Position
}
func (char *VRageRemoteCharacter) DistanceTo(other VRagePositionable) float64 {
	return Distance(char, other)
}
func (char *VRageRemoteCharacter) Stop() error {
	return char.client.StopCharacter(char.EntityID)
}

//--
//-- Players
//--

type VRageRemotePlayerListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemotePlayerList `json:"data"`
}
type VRageRemotePlayerList struct {
	Players []*VRageRemotePlayer
}
type VRageRemotePlayer struct {
	client       *VRageRemoteClient
	FactionTag   string
	PromoteLevel int
	Ping         float64
	SteamID      int64
	DisplayName  string
	FactionName  string
}

func (player *VRageRemotePlayer) Kick() error {
	return player.client.KickPlayer(player.SteamID)
}
func (player *VRageRemotePlayer) Ban() error {
	return player.client.BanPlayer(player.SteamID)
}

//--
//-- Asteroids
//--

type VRageRemoteAsteroidsListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteAsteroidsList `json:"data"`
}
type VRageRemoteAsteroidsList struct {
	Asteroids []*VRageRemoteAsteroid
}
type VRageRemoteAsteroid struct {
	client      *VRageRemoteClient
	DisplayName string
	EntityID    int64
	Position    VRagePosition
}

func (roid *VRageRemoteAsteroid) GetPosition() VRagePosition {
	return roid.Position
}
func (roid *VRageRemoteAsteroid) DistanceTo(other VRagePositionable) float64 {
	return Distance(roid, other)
}
func (roid *VRageRemoteAsteroid) Delete() error {
	return roid.client.DeleteAsteroid(roid.EntityID)
}

//--
//-- Floating Objects
//--

type VRageRemoteFloatingObjectListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteFloatingObjectList `json:"data"`
}
type VRageRemoteFloatingObjectList struct {
	FloatingObjects []*VRageRemoteFloatingObject
}
type VRageRemoteFloatingObject struct {
	client           *VRageRemoteClient
	DisplayName      string
	EntityID         int64 `json:"EntityId"`
	Kind             string
	Mass             float64
	Position         VRagePosition
	LinearSpeed      float64
	DistanceToPlayer float64
}

func (object *VRageRemoteFloatingObject) GetPosition() VRagePosition {
	return object.Position
}
func (object *VRageRemoteFloatingObject) DistanceTo(other VRagePositionable) float64 {
	return Distance(object, other)
}
func (object *VRageRemoteFloatingObject) Stop() error {
	return object.client.StopFloatingObject(object.EntityID)
}
func (object *VRageRemoteFloatingObject) Delete() error {
	return object.client.DeleteFloatingObject(object.EntityID)
}

// GetNearestGrids ordered by distance
func (object *VRageRemoteFloatingObject) GetNearestGrids() ([]*VRageRemoteGrid, error) {
	return object.GetNearestGridsIf(func(grid *VRageRemoteGrid) bool { return true })
}

// GetNearestGrids ordered by distance but only return grids which match a callback criteria
func (object *VRageRemoteFloatingObject) GetNearestGridsIf(fnc func(grid *VRageRemoteGrid) bool) ([]*VRageRemoteGrid, error) {
	gridsResponse, err := object.client.GetGrids()
	if err != nil {
		return nil, err
	}

	var grids []*VRageRemoteGrid
	for _, grid := range gridsResponse.Data.Grids {
		if fnc(grid) {
			grids = append(grids, grid)
		}
	}

	sort.SliceStable(grids, func(i, j int) bool {
		return object.Position.DistanceTo(grids[i].Position) < object.Position.DistanceTo(grids[j].Position)
	})

	return grids, nil
}

//--
//-- Grids
//--

type VRageRemoteGridListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteGridList `json:"data"`
}
type VRageRemoteGridList struct {
	Grids []*VRageRemoteGrid
}
type VRageRemoteGrid struct {
	client           *VRageRemoteClient
	DisplayName      string
	EntityID         int64 `json:"EntityId"`
	GridSize         string
	BlocksCount      int64
	Mass             float64
	Position         VRagePosition
	LinearSpeed      float64
	DistanceToPlayer float64
	OwnerSteamID     int64 `json:"OwnerSteamId"`
	OwnerDisplayName string
	IsPowered        bool
	PCU              int64
}

func (grid *VRageRemoteGrid) GetPosition() VRagePosition {
	return grid.Position
}
func (grid *VRageRemoteGrid) DistanceTo(other VRagePositionable) float64 {
	return Distance(grid, other)
}
func (grid *VRageRemoteGrid) Delete() error {
	return grid.client.DeleteGrid(grid.EntityID)
}
func (grid *VRageRemoteGrid) Stop() error {
	return grid.client.StopGrid(grid.EntityID)
}
func (grid *VRageRemoteGrid) PowerUp() error {
	return grid.client.PowerUpGrid(grid.EntityID)
}
func (grid *VRageRemoteGrid) PowerDown() error {
	return grid.client.PowerDownGrid(grid.EntityID)
}

//--
//-- Planets
//--

type VRageRemotePlanetListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemotePlanetList `json:"data"`
}
type VRageRemotePlanetList struct {
	Planets []*VRagePlanet
}
type VRagePlanet struct {
	client      *VRageRemoteClient
	DisplayName string
	EntityID    int64 `json:"EntityId"`
	Position    VRagePosition
}

func (planet *VRagePlanet) GetPosition() VRagePosition {
	return planet.Position
}
func (planet *VRagePlanet) DistanceTo(other VRagePositionable) float64 {
	return Distance(planet, other)
}
func (planet *VRagePlanet) Delete() error {
	return planet.client.DeletePlanet(planet.EntityID)
}

//--
//-- Chat Messages
//--

type VRageRemoteChatMessageListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteChatMessageList `json:"data"`
}
type VRageRemoteChatMessageList struct {
	Messages []*VRageChatMessage
}
type VRageChatMessage struct {
	SteamID     int64
	DisplayName string
	Content     string
	Timestamp   string
}

func (message *VRageChatMessage) GetRealTimestamp() time.Time {
	csTimestamp, _ := strconv.ParseInt(message.Timestamp, 10, 64)
	return timeFromTicks(csTimestamp)
}

//--
//-- Server Info
//--

type VRageRemoteServerInfoResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteServerInfo `json:"data"`
}
type VRageRemoteServerInfo struct {
	Game              string
	IsReady           bool
	Players           int64
	ServerID          int64 `json:"ServerId"`
	ServerName        string
	SimSpeed          float64
	SimulationCPULoad float64 `json:"SimulationCpuLoad"`
	TotalTime         float64
	PirateUsedPCU     int64
	UsedPCU           int64
	Version           string
	WorldName         string
}

//--
//-- Banned Players
//--

type VRageRemoteBannedPlayersListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteBannedPlayersList `json:"data"`
}
type VRageRemoteBannedPlayersList struct {
	BannedPlayers []*VRageBannedPlayer
}
type VRageBannedPlayer struct {
	SteamID     int64
	DisplayName string
}

//--
//-- Kicked Players
//--

type VRageRemoteKickedPlayersListResponse struct {
	*VRageRemoteResponse
	Data *VRageRemoteKickedPlayersList `json:"data"`
}
type VRageRemoteKickedPlayersList struct {
	KickedPlayers []*VRageKickedPlayer
}
type VRageKickedPlayer struct {
	SteamID     int64
	DisplayName string
	Time        int64
}

//--
//-- Client
//--

func (client *VRageRemoteClient) Save() error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("PATCH", "session", nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) SaveAs(name string) error {
	response := &VRageRemoteResponse{}
	query := make(url.Values)
	query.Add("savename", name)
	err := client.scanResponse("PATCH", "session", query, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) StopServer() error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", "server", nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetCharacters() (*VRageRemoteCharacterListResponse, error) {
	response := &VRageRemoteCharacterListResponse{}
	err := client.scanResponse("GET", "session/characters", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}

	for _, player := range response.Data.Characters {
		player.client = client
	}

	return response, nil
}
func (client *VRageRemoteClient) StopCharacter(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("PATCH", fmt.Sprintf("session/characters/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetPlayers() (*VRageRemotePlayerListResponse, error) {
	response := &VRageRemotePlayerListResponse{}
	err := client.scanResponse("GET", "session/players", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}

	for _, player := range response.Data.Players {
		player.client = client
	}

	return response, nil
}

func (client *VRageRemoteClient) GetAsteroids() (*VRageRemoteAsteroidsListResponse, error) {
	response := &VRageRemoteAsteroidsListResponse{}
	err := client.scanResponse("GET", "session/asteroids", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}

	for _, roid := range response.Data.Asteroids {
		roid.client = client
	}

	return response, nil
}
func (client *VRageRemoteClient) DeleteAsteroid(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("session/asteroids/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetFloatingObjects() (*VRageRemoteFloatingObjectListResponse, error) {
	response := &VRageRemoteFloatingObjectListResponse{}
	err := client.scanResponse("GET", "session/floatingObjects", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}

	for _, object := range response.Data.FloatingObjects {
		object.client = client
	}

	return response, nil
}
func (client *VRageRemoteClient) DeleteFloatingObject(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("session/floatingObjects/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) StopFloatingObject(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("PATCH", fmt.Sprintf("session/floatingObjects/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetGrids() (*VRageRemoteGridListResponse, error) {
	response := &VRageRemoteGridListResponse{}
	err := client.scanResponse("GET", "session/grids", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}

	for _, grid := range response.Data.Grids {
		grid.client = client
	}

	return response, nil
}
func (client *VRageRemoteClient) DeleteGrid(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("session/grids/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) StopGrid(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("PATCH", fmt.Sprintf("session/grids/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) PowerUpGrid(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("POST", fmt.Sprintf("session/poweredGrids/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) PowerDownGrid(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("session/poweredGrids/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetPlanets() (*VRageRemotePlanetListResponse, error) {
	response := &VRageRemotePlanetListResponse{}
	err := client.scanResponse("GET", "session/planets", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}

	for _, planet := range response.Data.Planets {
		planet.client = client
	}

	return response, nil
}
func (client *VRageRemoteClient) DeletePlanet(entityID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("session/planets/%d", entityID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetChat() (*VRageRemoteChatMessageListResponse, error) {
	response := &VRageRemoteChatMessageListResponse{}
	err := client.scanResponse("GET", "session/chat", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}
	return response, nil
}
func (client *VRageRemoteClient) SendChat(content string) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("POST", "session/chat", nil, content, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetServerInfo() (*VRageRemoteServerInfoResponse, error) {
	response := &VRageRemoteServerInfoResponse{}
	err := client.scanResponse("GET", "server", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}
	return response, nil
}
func (client *VRageRemoteClient) Ping() (time.Duration, error) {
	start := time.Now()
	response := &VRageRemoteResponse{}
	err := client.scanResponse("GET", "server/ping", nil, nil, response)
	if err != nil {
		return time.Duration(0), err
	}
	if response.Error != nil {
		return time.Duration(0), errors.New(response.Error.Message)
	}
	return time.Since(start), err
}

func (client *VRageRemoteClient) PromotePlayer(steamID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("POST", fmt.Sprintf("admin/promotedPlayers/%d", steamID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) DemotePlayer(steamID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("admin/promotedPlayers/%d", steamID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetBannedPlayers() (*VRageRemoteBannedPlayersListResponse, error) {
	response := &VRageRemoteBannedPlayersListResponse{}
	err := client.scanResponse("GET", "admin/bannedPlayers", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}
	return response, nil
}
func (client *VRageRemoteClient) BanPlayer(steamID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("POST", fmt.Sprintf("admin/bannedPlayers/%d", steamID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) UnbanPlayer(steamID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("admin/bannedPlayers/%d", steamID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) GetKickedPlayers() (*VRageRemoteKickedPlayersListResponse, error) {
	response := &VRageRemoteKickedPlayersListResponse{}
	err := client.scanResponse("GET", "admin/kickedPlayers", nil, nil, response)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.New(response.Error.Message)
	}
	return response, nil
}
func (client *VRageRemoteClient) KickPlayer(steamID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("POST", fmt.Sprintf("admin/kickedPlayers/%d", steamID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}
func (client *VRageRemoteClient) UnkickPlayer(steamID int64) error {
	response := &VRageRemoteResponse{}
	err := client.scanResponse("DELETE", fmt.Sprintf("admin/kickedPlayers/%d", steamID), nil, nil, response)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return nil
}

func (client *VRageRemoteClient) scanResponse(method string, resource string, query url.Values, body interface{}, responseStruct interface{}) error {
	requestMutex.Lock()
	defer requestMutex.Unlock()

	methodURL := client.BaseURL + "/" + resource

	if query != nil && len(query) > 0 {
		methodURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		requestBodyBytes, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewBuffer(requestBodyBytes)
	}

	request, err := http.NewRequest(method, client.RemoteAddress+methodURL, bodyReader)
	if err != nil {
		return err
	}

	date := time.Now().UTC().Format(time.RFC1123Z)
	nounce := fmt.Sprint(client.nonce)
	client.nonce++

	keyDecoded, err := base64.StdEncoding.DecodeString(client.Key)
	if err != nil {
		return errors.New("error decoding client key")
	}
	mac := hmac.New(sha1.New, keyDecoded)
	mac.Write([]byte(methodURL + "\r\n" + nounce + "\r\n" + date + "\r\n"))
	hash := mac.Sum(nil)
	encodedHash := base64.StdEncoding.EncodeToString(hash)

	if body != nil {
		request.Header.Add("Content-Type", "application/json")
	}
	request.Header.Add("Authorization", fmt.Sprintf("%s:%s", nounce, encodedHash))
	request.Header.Add("Date", date)

	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}

	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	err = json.Unmarshal(bodyBytes, responseStruct)
	if err != nil {
		return err
	}

	return nil
}

func NewVRageRemoteClient(remoteAddress string, key string) *VRageRemoteClient {
	return &VRageRemoteClient{
		BaseURL:       "/vrageremote/v1",
		RemoteAddress: remoteAddress,
		Key:           key,
		httpClient:    &http.Client{},
		nonce:         time.Now().UnixNano(),
	}
}

func Distance(a VRagePositionable, b VRagePositionable) float64 {
	ap := a.GetPosition()
	bp := b.GetPosition()
	x := bp.X - ap.X
	y := bp.Y - ap.Y
	z := bp.Z - ap.Z
	return math.Sqrt(x*x + y*y + z*z)
}
