package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)


type Conjur struct {
	Id                string               `json:"Id"`
	AdminAPIKey       string               `json:"AdminAPIKey"`
	ConjurContainer   *types.ContainerJSON `json:"-"`
	PostgresContainer *types.ContainerJSON `json:"-"`
	HostPort          string               `json:"HostPort"`
}

func RunContainer(
	ctx context.Context,
	cli *client.Client,
	imageName string,
	cmd []string,
	env []string,
	networkConfig *network.NetworkingConfig,
	portBindings nat.PortMap,
) (*types.ContainerJSON, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	_, err = cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		return nil, err
	}

	resp, err := cli.ContainerCreate(
		ctx,
		&container.Config{
			Image: imageName,
			Env: env,
			Cmd: cmd,
		},
		&container.HostConfig{
			PortBindings:    portBindings,
		},
		networkConfig,
		nil,
		"",
	)
	if err != nil {
		return nil, err
	}

	if err := cli.ContainerStart(
		ctx,
		resp.ID,
		types.ContainerStartOptions{},
	); err != nil {
		return nil, err
	}

	ctner, err := cli.ContainerInspect(
		ctx,
		resp.ID,
	)
	if err != nil {
		return nil, err
	}

	return &ctner, nil
}

func RunConjur(cli *client.Client) (*Conjur, error)  {
	ctx := context.Background()

	// Create a user defined network. Each of our containers
	// will be attached to this network.
	conjurID := strconv.Itoa(int(time.Now().Unix()))
	networkCreate := types.NetworkCreate{
		Attachable:     true,
		CheckDuplicate: true,
	}
	networkResult, err := cli.NetworkCreate(ctx, conjurID, networkCreate)
	defer func() {
		if err != nil {
			_ = cli.NetworkRemove(ctx, conjurID)
		}
	}()
	if err != nil {
		return nil, err
	}

	// Create NetworkingConfig.
	createNetworkingConfig := func(alias string) *network.NetworkingConfig{
		return &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				"net": {
					Aliases:   []string{alias},
					NetworkID: networkResult.ID,
				},
			},
		}
	}

	postgresContainer, err := RunContainer(
		ctx,
		cli,
		"postgres:10.15",
		nil,
		[]string{"POSTGRES_HOST_AUTH_METHOD=trust"},
		createNetworkingConfig("database"),
		nil,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = cli.ContainerRemove(ctx, postgresContainer.ID, types.ContainerRemoveOptions{})
		}
	}()


	// Wait for Postgres
	_, err = Exec(
		ctx,
		cli,
		postgresContainer.ID,
		[]string{"sh", "-c", `
until pg_isready
do
    echo "."
    sleep 1
done
`},
		time.Now().Add(5 * time.Second),
	)
	if err != nil {
		return nil, err
	}

	_, portBindings, err := nat.ParsePortSpecs([]string{"80"})
	if err != nil {
		return nil, err
	}

	dataKey, err := GenerateRandomString(32)
	if err != nil {
		return nil, err
	}

	conjurContainer, err := RunContainer(
		ctx,
		cli,
		"cyberark/conjur",
		[]string{"server"},
		[]string{
			"POSTGRES_HOST_AUTH_METHOD=trust",
			"DATABASE_URL=postgres://postgres@database/postgres",
			"CONJUR_DATA_KEY=" + dataKey,
			"CONJUR_AUTHENTICATORS=",
		},
		createNetworkingConfig("conjur"),
		portBindings,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = cli.ContainerRemove(ctx, conjurContainer.ID, types.ContainerRemoveOptions{})
		}
	}()

	// Get admin creds
	adminCredsResponse, err := Exec(
		ctx,
		cli,
		conjurContainer.ID,
		[]string{"sh", "-c", `
conjurctl wait 1>&2;
conjurctl account create demo | grep "^API key for admin:" | sed "s/^API key for admin: //"
`},
		time.Now().Add(5 * time.Second),
	)
	if err != nil {
		return nil, err
	}

	return &Conjur{
		Id:                conjurID,
		AdminAPIKey:       strings.TrimSpace(adminCredsResponse.StdOut),
		ConjurContainer:   conjurContainer,
		PostgresContainer: postgresContainer,
		HostPort:          conjurContainer.NetworkSettings.Ports["80/tcp"][0].HostPort,
	}, nil
}

type ExecResult struct {
	StdOut string
	StdErr string
	ExitCode int
}

func Exec(
	ctx context.Context,
	docker *client.Client,
	containerID string,
	command []string,
	deadline time.Time,
) (*ExecResult, error) {
	config :=  types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		Cmd: command,
	}

	if deadline.IsZero() {
		var ctxWithDeadlineCancel func()
		ctx, ctxWithDeadlineCancel = context.WithDeadline(ctx, deadline)
		defer ctxWithDeadlineCancel()
	}

	res, err := docker.ContainerExecCreate(ctx, containerID, config)
	if err != nil {
		return nil, err
	}

	return inspectExecResp(ctx, docker, res.ID)
}

func inspectExecResp(
	ctx context.Context,
	docker *client.Client,
	id string,
) (*ExecResult, error) {
	resp, err := docker.ContainerExecAttach(ctx, id, types.ExecStartCheck{})
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	// read the output
	var outBuf, errBuf bytes.Buffer
	outputDone := make(chan error)

	go func() {
		// StdCopy demultiplexes the stream into two buffers
		_, err = stdcopy.StdCopy(&outBuf, &errBuf, resp.Reader)
		outputDone <- err
	}()

	select {
	case err := <-outputDone:
		if err != nil {
			return nil, err
		}
		break

	case <-ctx.Done():
		return nil, ctx.Err()
	}

	stdout, err := ioutil.ReadAll(&outBuf)
	if err != nil {
		return nil, err
	}
	stderr, err := ioutil.ReadAll(&errBuf)
	if err != nil {
		return nil, err
	}

	res, err := docker.ContainerExecInspect(ctx, id)
	if err != nil {
		return nil, err
	}

	return &ExecResult{
		ExitCode: res.ExitCode,
		StdOut: string(stdout),
		StdErr: string(stderr),
	}, nil
}

// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}

// GenerateRandomString returns a URL-safe, base64 encoded
// securely generated random string.
// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
// Inspired by https://blog.questionable.services/article/generating-secure-random-numbers-crypto-rand/
func GenerateRandomString(s int) (string, error) {
	b, err := GenerateRandomBytes(s)
	return base64.URLEncoding.EncodeToString(b), err
}

