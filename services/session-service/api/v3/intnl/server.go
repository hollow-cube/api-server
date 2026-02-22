//go:generate go tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package intnl -generate types,strict-server,std-http-server openapi.yaml
///go:generate go tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o client.gen.go -package intnl -generate client openapi.yaml

package intnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/posthog"
	"github.com/hollow-cube/hc-services/services/session-service/pkg/kafkaModel"
	"github.com/nats-io/nats.go/jetstream"
	dto "github.com/prometheus/client_model/go"

	"github.com/google/go-github/v56/github"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/tracefx"
	playerService "github.com/hollow-cube/hc-services/services/player-service/api/v2/intnl"
	"github.com/hollow-cube/hc-services/services/session-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/handler"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/server"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/world"
	"github.com/prometheus/common/expfmt"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

var _ StrictServerInterface = (*serverImpl)(nil)

type serverImpl struct {
	invites   *handler.InviteManager
	jetStream *natsutil.JetStreamWrapper
	producer  kafkafx.SyncProducer
	gh        *github.Client

	queries *db.Queries

	playerClient  playerService.ClientWithResponsesInterface
	playerTracker *player.Tracker
	serverTracker *server.Tracker
	worldTracker  *world.Tracker

	k8s *kubernetes.Clientset
}

type ServerParams struct {
	fx.In

	Invites   *handler.InviteManager
	JetStream *natsutil.JetStreamWrapper
	Producer  kafkafx.SyncProducer
	GitHub    *github.Client

	Queries *db.Queries

	PlayerClient  playerService.ClientWithResponsesInterface
	PlayerTracker *player.Tracker
	ServerTracker *server.Tracker
	WorldTracker  *world.Tracker

	K8s *kubernetes.Clientset
}

func NewServer(params ServerParams) (StrictServerInterface, error) {
	err := params.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:       "MAP_JOINS",
		Subjects:   []string{"map-join.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     10 * time.Minute,
		Duplicates: 60 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &serverImpl{
		invites:       params.Invites,
		jetStream:     params.JetStream,
		producer:      params.Producer,
		gh:            params.GitHub,
		queries:       params.Queries,
		playerClient:  params.PlayerClient,
		playerTracker: params.PlayerTracker,
		serverTracker: params.ServerTracker,
		worldTracker:  params.WorldTracker,
		k8s:           params.K8s,
	}, nil
}

func (s *serverImpl) CreateSession(ctx context.Context, request CreateSessionRequestObject) (CreateSessionResponseObject, error) {
	punishmentResp, err := s.playerClient.GetActivePunishmentWithResponse(ctx, request.PlayerId, &playerService.GetActivePunishmentParams{
		PunishmentType: string(playerService.PunishmentTypeBan),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get active punishment: %w", err)
	}
	if punishmentResp.StatusCode() == 200 && punishmentResp.JSON200 != nil {
		// Return the body flat out not the parsed object so that this is not suceptible to the client reference going out of date.
		rawPlayerResponse := make(map[string]interface{})
		if err = json.Unmarshal(punishmentResp.Body, &rawPlayerResponse); err != nil {
			return nil, err
		}
		return &CreateSession403JSONResponse{rawPlayerResponse}, nil
	}

	// TODO check if already online

	// Get/create/update the player data object
	pd, pdRaw, err := s.getOrCreatePlayerData(ctx, request.PlayerId, request.Body.Username, request.Body.Ip, request.Body.Skin)
	if err != nil {
		return nil, err
	}
	pdRaw = s.updatePlayerDataFromJoin(pd, pdRaw, request.Body.Username, request.Body.Ip, request.Body.Skin)

	//if posthog.IsFeatureEnabledRemote(ctx, "maintenance", pd.Id) &&
	//	!pplayer.Has(pd.Permissions, pplayer.FlagGenericStaff) {
	//	return CreateSession401Response{}, nil
	//}

	protocolVersion := 0
	if request.Body.ProtocolVersion != nil {
		protocolVersion = *request.Body.ProtocolVersion
	}
	version := "unknown"
	if request.Body.Version != nil {
		version = *request.Body.Version
	}

	zap.S().Infow("creating session", "request", request, "version", version, "pvn", protocolVersion)

	skinTexture, skinSignature := "", ""
	if request.Body.Skin != nil {
		skinTexture = request.Body.Skin.Texture
		skinSignature = request.Body.Skin.Signature
	}
	_, err = s.playerTracker.CreateSession(ctx, request.Body.Proxy, nil, pd, skinTexture, skinSignature,
		request.Body.ConnectedHost, request.Body.Ip, protocolVersion, version)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return rawPlayerDataResponse(pdRaw), nil
}

func (s *serverImpl) DeleteSession(ctx context.Context, request DeleteSessionRequestObject) (DeleteSessionResponseObject, error) {
	duration, err := s.playerTracker.DeleteSession(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to delete session: %w", err)
	}
	if duration <= 0 {
		return DeleteSession404Response{}, nil
	}

	_, err = s.playerClient.UpdatePlayerDataWithResponse(ctx, request.PlayerId,
		playerService.PlayerDataUpdateRequest{PlaytimeInc: &duration})
	if err != nil {
		return nil, fmt.Errorf("failed to update player data: %w", err)
	}

	return DeleteSession200Response{}, nil
}

func (s *serverImpl) TransferSession(ctx context.Context, request TransferSessionRequestObject) (TransferSessionResponseObject, error) {
	pdResp, err := s.playerClient.GetPlayerDataWithResponse(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to get player data: %w", err)
	} else if pdResp.StatusCode() != 200 {
		return nil, fmt.Errorf("unexpected response from player service (%d): %s",
			pdResp.StatusCode(), pdResp.Body)
	}

	session, isFirstTransfer, err := s.playerTracker.TransferSession(ctx, request.PlayerId, &kafkaModel.Presence{
		Type:       kafkaModel.PresenceType(request.Body.Type),
		State:      request.Body.State,
		InstanceId: request.Body.Server,
		MapId:      request.Body.Map,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to transfer session: %w", err)
	} else if session == nil {
		return TransferSession404Response{}, nil
	}

	pdIf := make(map[string]interface{})
	if err = json.Unmarshal(pdResp.Body, &pdIf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal player data: %w", err)
	}

	return TransferSession201JSONResponse{TransferSessionResponseJSONResponse{
		Data:    pdIf,
		IsJoin:  isFirstTransfer,
		Session: sessionToApi(session),
	}}, nil
}

func (s *serverImpl) UpdateSessionProperties(ctx context.Context, request UpdateSessionPropertiesRequestObject) (UpdateSessionPropertiesResponseObject, error) {
	// TODO: Not sure if this needs locking or anything but for now its just vanish which definitely shouldn't
	session, err := s.playerTracker.GetSession(ctx, request.PlayerId)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch session: %w", err)
	} else if s == nil {
		return SessionNotFoundResponse{}, nil
	}

	var changed bool
	if request.Body.Hidden != nil && session.Hidden != *request.Body.Hidden {
		session.Hidden = *request.Body.Hidden
		changed = true
	}

	if changed {
		metadata := make(map[string]interface{})
		if request.Body.Metadata != nil {
			metadata = *request.Body.Metadata
		}
		err = s.playerTracker.UpdateSessionWithMetadata(ctx, session, metadata)
		if err != nil {
			return nil, err
		}
	}

	return UpdateSessionProperties200JSONResponse{
		SessionDataJSONResponse(sessionToApi(session)),
	}, nil
}

func (s *serverImpl) SyncServer(ctx context.Context, request SyncServerRequestObject) (SyncServerResponseObject, error) {
	allSessions, err := s.playerTracker.GetAllSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch all sessions: %w", err)
	}

	result := make(SessionList, len(allSessions))
	for i, session := range allSessions {
		result[i] = sessionToApi(session)
	}
	return SyncServer200JSONResponse(result), nil
}

func (s *serverImpl) JoinHub(ctx context.Context, request JoinHubRequestObject) (JoinHubResponseObject, error) {
	var excluding string
	if request.Body != nil && request.Body.Exclude != nil {
		excluding = *request.Body.Exclude
	}
	hubServers, err := s.serverTracker.GetActiveServersWithRole(ctx, "hub", excluding)
	if err != nil {
		return nil, fmt.Errorf("failed to get active hub servers: %w", err)
	} else if len(hubServers) == 0 {
		return JoinHub503Response{}, nil
	}

	state, err := s.serverTracker.GetState(ctx, hubServers[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get hub server state: %w", err)
	}

	return JoinHub200JSONResponse{MapJoinSuccessJSONResponse{
		Server:          state.ID,
		ServerClusterIP: state.ClusterIp,
	}}, nil
}

func (s *serverImpl) JoinMap(ctx context.Context, request JoinMapRequestObject) (JoinMapResponseObject, error) {
	state, err := s.findServerForMap(ctx, request)
	if errors.Is(err, world.ErrNoServerAvailable) {
		return JoinMap503Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to find server for map: %w", err)
	}

	posthog.Enqueue(posthog.Capture{
		Event:      "map_joined",
		DistinctId: request.Body.Player,
		Properties: posthog.NewProperties().
			Set("player_id", request.Body.Player).
			Set("map_id", request.Body.Map).
			Set("state", request.Body.State).
			Set("source", request.Body.Source).
			Set("$geoip_disable", true),
	})
	err = s.sendMapJoinMessage(ctx, model.MapJoinInfoMessage{
		ServerId: state.ID,
		PlayerId: request.Body.Player,
		MapId:    request.Body.Map,
		State:    request.Body.State,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send map join message: %w", err)
	}

	return JoinMap200JSONResponse{MapJoinSuccessJSONResponse{
		Server:          state.ID,
		ServerClusterIP: state.ClusterIp,
	}}, nil
}

func (s *serverImpl) findServerForMap(ctx context.Context, request JoinMapRequestObject) (*db.ServerState, error) {
	if request.Body.State == "playing" {
		// We can use a map isolate if player joining has the feature flag or they are specifically
		// using /join and the map exists as a map isolate already.
		// Note that this doesnt ensure they joined someone in the isolate version, but oh well its close enough.
		useMapIsolate := posthog.IsFeatureEnabledRemote(ctx, "map_isolate", request.Body.Player) ||
			(request.Body.Source == "join_command" && s.findExistingMapState(ctx, request.Body.Map))
		if useMapIsolate {
			var isolateOverride string
			if request.Body.Isolate != nil && request.Body.Isolate.Override != nil {
				isolateOverride = *request.Body.Isolate.Override
			}
			zap.S().Infow("using map isolate for request", "player", request.Body.Player, "map", request.Body.Map)
			return s.serverTracker.AllocServerForMap(ctx, request.Body.Map, isolateOverride)
		}
	}

	return s.worldTracker.FindServerForMap(ctx, request.Body.Map)
}

func (s *serverImpl) GetServerStats(ctx context.Context, request GetServerStatsRequestObject) (GetServerStatsResponseObject, error) {
	srv, err := s.serverTracker.GetState(ctx, request.ServerId)
	if err != nil {
		return nil, fmt.Errorf("failed to get server state: %w", err)
	} else if srv == nil {
		return GetServerStats404Response{}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+srv.ClusterIp+":9124/metrics", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	res, err := tracefx.DefaultHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}
	defer res.Body.Close()

	parser := expfmt.TextParser{}
	metrics, err := parser.TextToMetricFamilies(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metrics: %w", err)
	}

	containerUsed := int(getMetricValue(metrics, "process_resident_memory_bytes", nil))
	vmUsed := int(getMetricValue(metrics, "jvm_memory_bytes_used", map[string]string{"area": "heap"}))
	vmCommitted := int(getMetricValue(metrics, "jvm_memory_bytes_committed", map[string]string{"area": "heap"}))
	vmMax := int(getMetricValue(metrics, "jvm_memory_bytes_max", map[string]string{"area": "heap"}))
	vmPercent := float32(float64(vmUsed) / float64(vmCommitted) * 100)
	offHeap := containerUsed - vmCommitted

	return &GetServerStats200JSONResponse{
		ContainerUsed: &containerUsed,
		OffHeap:       &offHeap,
		VmCommitted:   &vmCommitted,
		VmMax:         &vmMax,
		VmPercent:     &vmPercent,
		VmUsed:        &vmUsed,
	}, nil
}

func getMetricValue(families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	family, ok := families[name]
	if !ok {
		return 0
	}

	for _, metric := range family.Metric {
		if matchLabels(metric.Label, labels) {
			if metric.Gauge != nil {
				return metric.Gauge.GetValue()
			}
			if metric.Counter != nil {
				return metric.Counter.GetValue()
			}
			if metric.Untyped != nil {
				return metric.Untyped.GetValue()
			}
		}
	}
	return 0
}

func matchLabels(metricLabels []*dto.LabelPair, want map[string]string) bool {
	if want == nil {
		return true
	}
	labelMap := make(map[string]string)
	for _, lp := range metricLabels {
		labelMap[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if labelMap[k] != v {
			return false
		}
	}
	return true
}

func (s *serverImpl) findExistingMapState(ctx context.Context, mapId string) bool {
	_, err := s.queries.GetFirstServerStateByMap(ctx, mapId)
	return err == nil
}

func (s *serverImpl) getOrCreatePlayerData(ctx context.Context, playerId, username, ip string, skin *PlayerSkin) (*playerService.PlayerData, []byte, error) {
	pdResp, err := s.playerClient.GetPlayerDataWithResponse(ctx, playerId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get player data: %w", err)
	}
	if pdResp.StatusCode() == 200 {
		return pdResp.JSON200, pdResp.Body, nil
	} else if pdResp.StatusCode() == 404 {
		var playerSkin *playerService.PlayerSkin
		if skin != nil {
			playerSkin = &playerService.PlayerSkin{
				Texture:   skin.Texture,
				Signature: skin.Signature,
			}
		}

		createResp, err := s.playerClient.CreatePlayerDataWithResponse(ctx, playerService.CreatePlayerDataJSONRequestBody{
			Id:       playerId,
			Ip:       ip,
			Username: username,
			Skin:     playerSkin,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create player data: %w", err)
		} else if createResp.StatusCode() != 201 {
			return nil, nil, fmt.Errorf("failed to create player data (%d): %s", createResp.StatusCode(), createResp.Body)
		}

		return createResp.JSON201, createResp.Body, nil
	}

	return nil, nil, fmt.Errorf("failed to get player data (%d): %s", pdResp.StatusCode(), pdResp.Body)
}

func (s *serverImpl) updatePlayerDataFromJoin(pd *playerService.PlayerData, pdRaw []byte, newUsername, newIp string, newSkin *PlayerSkin) []byte {
	syncUpdate := false
	now := time.Now()
	pd.LastOnline = now
	pdUpdate := playerService.PlayerDataUpdateRequest{LastOnline: &now}

	newIps := []string{newIp}
	pdUpdate.IpHistory = &newIps

	if pd.Username != newUsername {
		pd.Username = newUsername
		pdUpdate.Username = &newUsername
		syncUpdate = true
	}
	if newSkin != nil && (pd.Skin == nil || pd.Skin.Texture != newSkin.Texture || pd.Skin.Signature != newSkin.Signature) {
		pd.Skin = &playerService.PlayerSkin{
			Texture:   newSkin.Texture,
			Signature: newSkin.Signature,
		}
		pdUpdate.Skin = pd.Skin
	}

	// If the username changed we need to block here while updating it, otherwise no need
	updateFunc := func() {
		// Do not use request context because it gets cancelled
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
		defer cancel()
		_, err := s.playerClient.UpdatePlayerDataWithResponse(ctx, pd.Id, pdUpdate)
		if err != nil {
			zap.S().Errorw("failed to update player data", "update", pdUpdate, "error", err)
		}
	}
	if syncUpdate {
		updateFunc()
	} else {
		go updateFunc()
	}

	if pdUpdate.Username == nil && pdUpdate.Skin == nil {
		return pdRaw
	}

	// This is kind of weird logic to pass through this "raw" object, but its relevant to ensure that this service's
	// version of the PlayerData object can correctly pass down backwards compatible changes.
	// We just parse the raw object and update the username field.
	var raw map[string]interface{}
	if err := json.Unmarshal(pdRaw, &raw); err != nil {
		zap.S().Errorw("failed to unmarshal player data", "error", err)
		return pdRaw
	}
	raw["username"] = newUsername
	raw["skin"] = newSkin
	newRaw, err := json.Marshal(raw)
	if err != nil {
		zap.S().Errorw("failed to marshal player data", "error", err)
		return pdRaw
	}
	return newRaw
}

func (s *serverImpl) sendMapJoinMessage(ctx context.Context, msg model.MapJoinInfoMessage) error {
	if err := s.jetStream.PublishJSONAsync(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish invite message: %w", err)
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to encode map join message: %w", err)
	}

	return s.producer.WriteMessages(ctx, kafka.Message{
		Topic: kafkafx.TopicMapJoin,
		Value: raw,
	})
}

type rawPlayerDataResponse []byte

func (response rawPlayerDataResponse) VisitCreateSessionResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)

	_, err := w.Write(response)
	return err
}

func sessionToApi(session *db.PlayerSession) PlayerSession {
	serverId := ""
	if session.ServerID != nil {
		serverId = *session.ServerID
	}
	var presence Presence
	if session.PType != nil {
		presence = Presence{
			Type:       string(*session.PType),
			State:      *session.PState,
			InstanceId: *session.PInstanceID,
			MapId:      *session.PMapID,
		}
	}
	return PlayerSession{
		PlayerId:  session.PlayerID,
		CreatedAt: session.CreatedAt.Format(time.RFC3339),
		ProxyId:   session.ProxyID,
		ServerId:  serverId,
		Hidden:    session.Hidden,
		Username:  *session.Username,
		Skin: &PlayerSkin{
			Texture:   session.SkinTexture,
			Signature: session.SkinSignature,
		},
		Presence: presence,
	}
}
