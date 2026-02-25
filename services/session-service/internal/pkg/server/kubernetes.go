package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/go-github/v56/github"
	"github.com/google/uuid"
	mapIntnlV3 "github.com/hollow-cube/hc-services/services/session-service/api/mapsV3/intnl"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

var (
	defaultSecurityContext = &coreV1.SecurityContext{
		RunAsNonRoot:             new(true),
		RunAsUser:                new(int64(65532)),
		AllowPrivilegeEscalation: new(false),
		Capabilities: &coreV1.Capabilities{
			Drop: []coreV1.Capability{"ALL"},
		},
		SeccompProfile: &coreV1.SeccompProfile{
			Type: coreV1.SeccompProfileTypeRuntimeDefault,
		},
	}
	defaultIsolatePorts = []coreV1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: 9124,
			Protocol:      coreV1.ProtocolTCP,
		},
		{
			Name:          "minecraft",
			ContainerPort: 25565,
			Protocol:      coreV1.ProtocolTCP,
		},
	}
)

func (t *Tracker) allocMapServerPod(ctx context.Context, mapId, isolateOverride string) (string, string, error) {
	ctx, span := otelTracer.Start(ctx, "server.allocMapServerPod")
	defer span.End()

	imageTag, env, err := findImageTag()
	if err != nil {
		return "", "", err
	}
	zap.S().Infow("found image tag", "imageTag", imageTag, "env", env)

	if isolateOverride != "" {
		runs, _, err := t.gh.Actions.ListWorkflowRunsByFileName(ctx, "hollow-cube", "mapmaker", "pr-isolate.yml", &github.ListWorkflowRunsOptions{
			Branch: isolateOverride,
			Status: "completed",
			ListOptions: github.ListOptions{
				PerPage: 1,
			},
		})
		if err != nil {
			return "", "", fmt.Errorf("failed to list workflow runs: %w", err)
		}
		if runs.GetTotalCount() == 0 {
			return "", "", fmt.Errorf("no workflow runs found for branch %s", isolateOverride)
		}

		imageTag = fmt.Sprintf("mworzala/mapmaker-map-isolate:preview-%s", *runs.WorkflowRuns[0].HeadSHA)
		zap.S().Infow("using workflow run image", "imageTag", imageTag)
	}

	m, err := t.mapStore.GetMapById(ctx, mapId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", "", err
	}

	instanceName := t.isolateConfig.DefaultSize
	instanceSize := t.isolateConfig.Instances[t.isolateConfig.DefaultSize]
	if rawWorldSize, ok := t.isolateConfig.WorldSizeMapping[string(mapIntnlV3.MapSizeToAPI(m.Size))]; ok && rawWorldSize != "" {
		if worldSize, ok := t.isolateConfig.Instances[rawWorldSize]; ok {
			instanceName = rawWorldSize
			instanceSize = worldSize
		}
	}

	// gross, should handle this in sqlc gen
	extra := make(map[string]interface{})
	if m.OptExtra != nil {
		_ = json.Unmarshal(m.OptExtra, &extra)
	}

	if rawOverrideSize, ok := extra["instance_size"].(string); ok && rawOverrideSize != "" {
		if overrideSize, ok := t.isolateConfig.Instances[rawOverrideSize]; ok {
			instanceName = rawOverrideSize
			instanceSize = overrideSize
		}
	}
	if instanceSize.Memory == 0 {
		// This is definitely a configuration issue, so im ok with panicking here.
		panic("invalid instance config (memory is 0 or default is missing or invalid)")
	}

	podEnv := make([]coreV1.EnvVar, 0, len(env))
	for k, v := range env {
		podEnv = append(podEnv, coreV1.EnvVar{
			Name:  k,
			Value: v,
		})
	}
	podEnv = append(podEnv, coreV1.EnvVar{
		Name:  "MAPMAKER_INSTANCE_SIZE",
		Value: instanceName,
	})

	// TODO cpu
	memoryLimit := resource.MustParse(fmt.Sprintf("%dMi", instanceSize.Memory))
	jvmMemoryLimit := int(float64(instanceSize.Memory) * 0.5)

	podSpec := coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name: fmt.Sprintf("map-%s-%s", mapId[0:8], uuid.NewString()[0:4]),
			Labels: map[string]string{
				"mapmaker.hollowcube.net/role": "map-isolate",
			},
		},
		Spec: coreV1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": "ovh-02",
			},
			ServiceAccountName:           "mapmaker-map-isolate",
			AutomountServiceAccountToken: new(false),
			RestartPolicy:                coreV1.RestartPolicyNever,
			ImagePullSecrets: []coreV1.LocalObjectReference{
				{Name: "dockerio"},
			},
			Containers: []coreV1.Container{
				{
					Name:            "map-isolate",
					Image:           imageTag,
					ImagePullPolicy: coreV1.PullIfNotPresent,
					SecurityContext: defaultSecurityContext,
					Ports:           defaultIsolatePorts,
					Env:             podEnv,
					Resources: coreV1.ResourceRequirements{
						Limits: map[coreV1.ResourceName]resource.Quantity{
							coreV1.ResourceMemory: memoryLimit,
						},
						Requests: map[coreV1.ResourceName]resource.Quantity{
							coreV1.ResourceMemory: memoryLimit,
						},
					},
					Command: []string{
						"./map-isolate",
						fmt.Sprintf("-Xms%dM", jvmMemoryLimit),
						fmt.Sprintf("-Xmx%dM", jvmMemoryLimit),
						mapId,
					},
				},
			},
		},
	}

	pod, err := t.k8s.CoreV1().Pods("mapmaker").Create(ctx, &podSpec, metaV1.CreateOptions{})
	if err != nil {
		zap.S().Error("failed to create pod", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", "", err
	}

	zap.S().Info("created pod", "pod", pod.Name)
	return pod.Name, pod.ResourceVersion, nil
}

func deleteMapServerPod(ctx context.Context, k8s *kubernetes.Clientset, podName string) error {
	err := k8s.CoreV1().Pods("mapmaker").Delete(ctx, podName, metaV1.DeleteOptions{})
	if err != nil {
		zap.S().Error("failed to delete pod", zap.Error(err))
		return err
	}
	zap.S().Info("deleted pod", "pod", podName)
	return nil
}

type mapIsolateConfig struct {
	Image string            `yaml:"image"`
	Env   map[string]string `yaml:"env"`
}

func findImageTag() (string, map[string]string, error) {
	// Try to read the more complex config present in prod.
	const prodFileName = "/etc/map-isolate/mapmaker-map-isolate-config.yaml"
	if _, err := os.Stat(prodFileName); err == nil {
		text, err := os.ReadFile(prodFileName)
		if err != nil {
			return "", nil, fmt.Errorf("failed to read %s: %w", prodFileName, err)
		}

		var config mapIsolateConfig
		if err = yaml.Unmarshal(text, &config); err != nil {
			return "", nil, fmt.Errorf("failed to parse %s: %w", prodFileName, err)
		}

		return config.Image, config.Env, nil
	}

	// Fall back to the handling we use in tilt
	// TODO: figure out how to make tilt update the image name in the yaml.
	const tiltFileName = "/etc/map-isolate/map-isolate-image"
	data, err := os.ReadFile(tiltFileName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read %s: %w", tiltFileName, err)
	}
	return string(data), nil, nil
}
