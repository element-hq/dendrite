package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/codeclysm/extract"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

var (
	flagTempDir          = flag.String("tmp", "tmp", "Path to temporary directory to dump tarballs to")
	flagFrom             = flag.String("from", "HEAD-1", "The version to start from e.g '0.3.1'. If 'HEAD-N' then starts N versions behind HEAD.")
	flagTo               = flag.String("to", "HEAD", "The version to end on e.g '0.3.3'.")
	flagBuildConcurrency = flag.Int("build-concurrency", runtime.NumCPU(), "The amount of build concurrency when building images")
	flagHead             = flag.String("head", "", "Location to a dendrite repository to treat as HEAD instead of Github")
	flagDockerHost       = flag.String("docker-host", "localhost", "The hostname of the docker client. 'localhost' if running locally, 'host.docker.internal' if running in Docker.")
	flagDirect           = flag.Bool("direct", false, "If a direct upgrade from the defined FROM version to TO should be done")
	flagSqlite           = flag.Bool("sqlite", false, "Test SQLite instead of PostgreSQL")
	flagRepository       = flag.String("repository", "element-hq/dendrite", "The base repository to use when running upgrade tests.")
	alphaNumerics        = regexp.MustCompile("[^a-zA-Z0-9]+")
)

const HEAD = "HEAD"

// The binary was renamed after v0.11.1, so everything after that should use the new name
var binaryChangeVersion, _ = semver.NewVersion("v0.11.1")
var latest, _ = semver.NewVersion("v6.6.6") // Dummy version, used as "HEAD"

// Embed the Dockerfile to use when building dendrite versions.
// We cannot use the dockerfile associated with the repo with each version sadly due to changes in
// Docker versions. Specifically, earlier Dendrite versions are incompatible with newer Docker clients
// due to the error:
// When using COPY with more than one source file, the destination must be a directory and end with a /
// We need to run a postgres anyway, so use the dockerfile associated with Complement instead.
const DockerfilePostgreSQL = `FROM golang:1.24-bookworm as build
RUN apt-get update && apt-get install -y postgresql
WORKDIR /build
ARG BINARY

# Copy the build context to the repo as this is the right dendrite code. This is different to the
# Complement Dockerfile which wgets a branch.
COPY . .

RUN go build ./cmd/${BINARY}
RUN go build ./cmd/generate-keys
RUN go build ./cmd/generate-config
RUN go build ./cmd/create-account
RUN ./generate-config --ci --db "user=postgres database=postgres host=/var/run/postgresql/" > dendrite.yaml
RUN ./generate-keys --private-key matrix_key.pem --tls-cert server.crt --tls-key server.key

# No password when connecting to Postgres
RUN sed -i "s%peer%trust%g" /etc/postgresql/15/main/pg_hba.conf
# Bump up max conns for moar concurrency
RUN sed -i 's/max_connections = 100/max_connections = 2000/g' /etc/postgresql/15/main/postgresql.conf
RUN sed -i 's/max_open_conns:.*$/max_open_conns: 100/g' dendrite.yaml

# This entry script starts postgres, waits for it to be up then starts dendrite
RUN echo '\
#!/bin/bash -eu \n\
pg_lsclusters \n\
pg_ctlcluster 15 main start \n\
 \n\
until pg_isready \n\
do \n\
  echo "Waiting for postgres"; \n\
  sleep 1; \n\
done \n\
 \n\
sed -i "s/server_name: localhost/server_name: ${SERVER_NAME}/g" dendrite.yaml \n\
PARAMS="--tls-cert server.crt --tls-key server.key --config dendrite.yaml" \n\
./${BINARY} --really-enable-open-registration ${PARAMS} || ./${BINARY} ${PARAMS} \n\
' > run_dendrite.sh && chmod +x run_dendrite.sh

ENV SERVER_NAME=localhost
ENV BINARY=dendrite
EXPOSE 8008 8448
CMD /build/run_dendrite.sh`

const DockerfileSQLite = `FROM golang:1.24-bookworm as build
RUN apt-get update && apt-get install -y postgresql
WORKDIR /build
ARG BINARY

# Copy the build context to the repo as this is the right dendrite code. This is different to the
# Complement Dockerfile which wgets a branch.
COPY . .

RUN go build ./cmd/${BINARY}
RUN go build ./cmd/generate-keys
RUN go build ./cmd/generate-config
RUN go build ./cmd/create-account
RUN ./generate-config --ci > dendrite.yaml
RUN ./generate-keys --private-key matrix_key.pem --tls-cert server.crt --tls-key server.key

# Make sure the SQLite databases are in a persistent location, we're already mapping
# the postgresql folder so let's just use that for simplicity
RUN sed -i "s%connection_string:.file:%connection_string: file:\/var\/lib\/postgresql\/15\/main\/%g" dendrite.yaml

# This entry script starts postgres, waits for it to be up then starts dendrite
RUN echo '\
sed -i "s/server_name: localhost/server_name: ${SERVER_NAME}/g" dendrite.yaml \n\
PARAMS="--tls-cert server.crt --tls-key server.key --config dendrite.yaml" \n\
./${BINARY} --really-enable-open-registration ${PARAMS} || ./${BINARY} ${PARAMS} \n\
' > run_dendrite.sh && chmod +x run_dendrite.sh

ENV SERVER_NAME=localhost
ENV BINARY=dendrite
EXPOSE 8008 8448
CMD /build/run_dendrite.sh `

func dockerfile() []byte {
	if *flagSqlite {
		return []byte(DockerfileSQLite)
	}
	return []byte(DockerfilePostgreSQL)
}

const dendriteUpgradeTestLabel = "dendrite_upgrade_test"

// downloadArchive downloads an arbitrary github archive of the form:
//
//	https://github.com/element-hq/dendrite/archive/v0.3.11.tar.gz
//
// and re-tarballs it without the top-level directory which contains branch information. It inserts
// the contents of `dockerfile` as a root file `Dockerfile` in the re-tarballed directory such that
// you can directly feed the retarballed archive to `ImageBuild` to have it run said dockerfile.
// Returns the tarball buffer on success.
func downloadArchive(cli *http.Client, tmpDir, archiveURL string, dockerfile []byte) (*bytes.Buffer, error) {
	resp, err := cli.Get(archiveURL)
	if err != nil {
		return nil, err
	}
	// nolint:errcheck
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("got HTTP %d", resp.StatusCode)
	}
	_ = os.RemoveAll(tmpDir)
	if err = os.Mkdir(tmpDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to make temporary directory: %s", err)
	}
	// nolint:errcheck
	defer os.RemoveAll(tmpDir)
	// dump the tarball temporarily, stripping the top-level directory
	err = extract.Archive(context.Background(), resp.Body, tmpDir, func(inPath string) string {
		// remove top level
		segments := strings.Split(inPath, "/")
		return strings.Join(segments[1:], "/")
	})
	if err != nil {
		return nil, err
	}
	// add top level Dockerfile
	err = os.WriteFile(path.Join(tmpDir, "Dockerfile"), dockerfile, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to inject /Dockerfile: %w", err)
	}
	// now re-tarball it :/
	var tarball bytes.Buffer
	err = compress(tmpDir, &tarball)
	if err != nil {
		return nil, err
	}
	return &tarball, nil
}

// buildDendrite builds Dendrite on the branchOrTagName given. Returns the image ID or an error
func buildDendrite(httpClient *http.Client, dockerClient *client.Client, tmpDir string, branchOrTagName, binary, repository string) (string, error) {
	var tarball *bytes.Buffer
	var err error
	// If a custom HEAD location is given, use that, else pull from github. Mostly useful for CI
	// where we want to use the working directory.
	if branchOrTagName == HEAD && *flagHead != "" {
		log.Printf("%s: Using %s as HEAD", branchOrTagName, *flagHead)
		// add top level Dockerfile
		err = os.WriteFile(path.Join(*flagHead, "Dockerfile"), dockerfile(), os.ModePerm)
		if err != nil {
			return "", fmt.Errorf("custom HEAD: failed to inject /Dockerfile: %w", err)
		}
		// now tarball it
		var buffer bytes.Buffer
		err = compress(*flagHead, &buffer)
		if err != nil {
			return "", fmt.Errorf("failed to tarball custom HEAD %s : %s", *flagHead, err)
		}
		tarball = &buffer
	} else {
		log.Printf("%s: Downloading version %s to %s\n", branchOrTagName, branchOrTagName, tmpDir)
		// pull an archive, this contains a top-level directory which screws with the build context
		// which we need to fix up post download
		u := fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", repository, branchOrTagName)
		tarball, err = downloadArchive(httpClient, tmpDir, u, dockerfile())
		if err != nil {
			return "", fmt.Errorf("failed to download archive %s: %w", u, err)
		}
		log.Printf("%s: %s => %d bytes\n", branchOrTagName, u, tarball.Len())
	}

	log.Printf("%s: Building version %s\n", branchOrTagName, branchOrTagName)
	res, err := dockerClient.ImageBuild(context.Background(), tarball, types.ImageBuildOptions{
		Tags: []string{"dendrite-upgrade"},
		BuildArgs: map[string]*string{
			"BINARY": &binary,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to start building image: %s", err)
	}
	// nolint:errcheck
	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	// {"aux":{"ID":"sha256:247082c717963bc2639fc2daed08838d67811ea12356cd4fda43e1ffef94f2eb"}}
	var imageID string
	for decoder.More() {
		var dl struct {
			Stream string                 `json:"stream"`
			Aux    map[string]interface{} `json:"aux"`
		}
		if err := decoder.Decode(&dl); err != nil {
			return "", fmt.Errorf("failed to decode build image output line: %w", err)
		}
		if len(strings.TrimSpace(dl.Stream)) > 0 {
			log.Printf("%s: %s", branchOrTagName, dl.Stream)
		}
		if dl.Aux != nil {
			imgID, ok := dl.Aux["ID"]
			if ok {
				imageID = imgID.(string)
			}
		}
	}
	return imageID, nil
}

func getAndSortVersionsFromGithub(httpClient *http.Client, repository string) (semVers []*semver.Version, err error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/tags", repository)

	var res *http.Response
	for i := 0; i < 3; i++ {
		res, err = httpClient.Get(u)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == 200 {
			break
		}
		log.Printf("Github API returned HTTP %d, retrying\n", res.StatusCode)
		time.Sleep(time.Second * 5)
	}

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned HTTP %d", u, res.StatusCode)
	}
	resp := []struct {
		Name string `json:"name"`
	}{}
	if err = json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	for _, r := range resp {
		v, err := semver.NewVersion(r.Name)
		if err != nil {
			continue // not a semver, that's ok and isn't an error, we allow tags that aren't semvers
		}
		semVers = append(semVers, v)
	}
	sort.Sort(semver.Collection(semVers))
	return semVers, nil
}

func calculateVersions(cli *http.Client, from, to, repository string, direct bool) []*semver.Version {
	semvers, err := getAndSortVersionsFromGithub(cli, repository)
	if err != nil {
		log.Fatalf("failed to collect semvers from github: %s", err)
	}
	// snip the lower bound depending on --from
	if from != "" {
		if strings.HasPrefix(from, "HEAD-") {
			var headN int
			headN, err = strconv.Atoi(strings.TrimPrefix(from, "HEAD-"))
			if err != nil {
				log.Fatalf("invalid --from, try 'HEAD-1'")
			}
			if headN >= len(semvers) {
				log.Fatalf("only have %d versions, but asked to go to HEAD-%d", len(semvers), headN)
			}
			if headN > 0 {
				semvers = semvers[len(semvers)-headN:]
			}
		} else {
			fromVer, err := semver.NewVersion(from)
			if err != nil {
				log.Fatalf("invalid --from: %s", err)
			}
			i := 0
			for i = 0; i < len(semvers); i++ {
				if semvers[i].LessThan(fromVer) {
					continue
				}
				break
			}
			semvers = semvers[i:]
		}
	}
	if to != "" && to != HEAD {
		toVer, err := semver.NewVersion(to)
		if err != nil {
			log.Fatalf("invalid --to: %s", err)
		}
		var i int
		for i = len(semvers) - 1; i >= 0; i-- {
			if semvers[i].GreaterThan(toVer) {
				continue
			}
			break
		}
		semvers = semvers[:i+1]
	}

	if to == HEAD {
		semvers = append(semvers, latest)
	}
	if direct {
		semvers = []*semver.Version{semvers[0], semvers[len(semvers)-1]}
	}
	return semvers
}

func buildDendriteImages(httpClient *http.Client, dockerClient *client.Client, baseTempDir, repository string, concurrency int, versions []*semver.Version) map[string]string {
	// concurrently build all versions, this can be done in any order. The mutex protects the map
	branchToImageID := make(map[string]string)
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(concurrency)
	ch := make(chan *semver.Version, len(versions))
	for _, branchName := range versions {
		ch <- branchName
	}
	close(ch)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for version := range ch {
				branchName, binary := versionToBranchAndBinary(version)
				log.Printf("Building version %s with binary %s", branchName, binary)
				tmpDir := baseTempDir + alphaNumerics.ReplaceAllString(branchName, "")
				imgID, err := buildDendrite(httpClient, dockerClient, tmpDir, branchName, binary, repository)
				if err != nil {
					log.Fatalf("%s: failed to build dendrite image: %s", version, err)
				}
				mu.Lock()
				branchToImageID[branchName] = imgID
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return branchToImageID
}

func runImage(dockerClient *client.Client, volumeName string, branchNameToImageID map[string]string, version *semver.Version) (csAPIURL, containerID string, err error) {
	branchName, binary := versionToBranchAndBinary(version)
	imageID := branchNameToImageID[branchName]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	body, err := dockerClient.ContainerCreate(ctx, &container.Config{
		Image: imageID,
		Env:   []string{"SERVER_NAME=hs1", fmt.Sprintf("BINARY=%s", binary)},
		Labels: map[string]string{
			dendriteUpgradeTestLabel: "yes",
		},
	}, &container.HostConfig{
		PublishAllPorts: true,
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/var/lib/postgresql/15/main",
			},
		},
	}, nil, nil, "dendrite_upgrade_test_"+branchName)
	if err != nil {
		return "", "", fmt.Errorf("failed to ContainerCreate: %s", err)
	}
	containerID = body.ID

	err = dockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to ContainerStart: %s", err)
	}
	inspect, err := dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", "", err
	}
	csapiPortInfo, ok := inspect.NetworkSettings.Ports[nat.Port("8008/tcp")]
	if !ok {
		return "", "", fmt.Errorf("port 8008 not exposed - exposed ports: %v", inspect.NetworkSettings.Ports)
	}
	baseURL := fmt.Sprintf("http://%s:%s", *flagDockerHost, csapiPortInfo[0].HostPort)
	versionsURL := fmt.Sprintf("%s/_matrix/client/versions", baseURL)
	// hit /versions to check it is up
	var lastErr error
	for i := 0; i < 500; i++ {
		var res *http.Response
		res, err = http.Get(versionsURL)
		if err != nil {
			lastErr = fmt.Errorf("GET %s => error: %s", versionsURL, err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if res.StatusCode != 200 {
			lastErr = fmt.Errorf("GET %s => HTTP %s", versionsURL, res.Status)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		lastErr = nil
		break
	}
	logs, err := dockerClient.ContainerLogs(context.Background(), containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	// ignore errors when cannot get logs, it's just for debugging anyways
	if err == nil {
		go func() {
			for {
				if body, err := io.ReadAll(logs); err == nil && len(body) > 0 {
					log.Printf("%s: %s", version, string(body))
				} else {
					return
				}
			}
		}()
	}
	return baseURL, containerID, lastErr
}

func destroyContainer(dockerClient *client.Client, containerID string) {
	err := dockerClient.ContainerRemove(context.TODO(), containerID, container.RemoveOptions{
		Force: true,
	})
	if err != nil {
		log.Printf("failed to remove container %s : %s", containerID, err)
	}
}

func loadAndRunTests(dockerClient *client.Client, volumeName string, v *semver.Version, branchToImageID map[string]string) error {
	csAPIURL, containerID, err := runImage(dockerClient, volumeName, branchToImageID, v)
	if err != nil {
		return fmt.Errorf("failed to run container for branch %v: %v", v, err)
	}
	defer destroyContainer(dockerClient, containerID)
	log.Printf("URL %s -> %s \n", csAPIURL, containerID)
	if err = runTests(csAPIURL, v); err != nil {
		return fmt.Errorf("failed to run tests on version %s: %s", v, err)
	}

	err = testCreateAccount(dockerClient, v, containerID)
	if err != nil {
		return err
	}
	return nil
}

// test that create-account is working
func testCreateAccount(dockerClient *client.Client, version *semver.Version, containerID string) error {
	branchName, _ := versionToBranchAndBinary(version)
	createUser := strings.ToLower("createaccountuser-" + branchName)
	log.Printf("%s: Creating account %s with create-account\n", branchName, createUser)

	respID, err := dockerClient.ContainerExecCreate(context.Background(), containerID, types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		Cmd: []string{
			"/build/create-account",
			"-username", createUser,
			"-password", "someRandomPassword",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to ContainerExecCreate: %w", err)
	}

	response, err := dockerClient.ContainerExecAttach(context.Background(), respID.ID, types.ExecStartCheck{})
	if err != nil {
		return fmt.Errorf("failed to attach to container: %w", err)
	}
	defer response.Close()

	data, err := io.ReadAll(response.Reader)
	if err != nil {
		return err
	}

	if !bytes.Contains(data, []byte("AccessToken")) {
		return fmt.Errorf("failed to create-account: %s", string(data))
	}
	return nil
}

func versionToBranchAndBinary(version *semver.Version) (branchName, binary string) {
	binary = "dendrite-monolith-server"
	branchName = version.Original()
	if version.GreaterThan(binaryChangeVersion) {
		binary = "dendrite"
		if version.Equal(latest) {
			branchName = HEAD
		}
	}
	return
}

func verifyTests(dockerClient *client.Client, volumeName string, versions []*semver.Version, branchToImageID map[string]string) error {
	lastVer := versions[len(versions)-1]
	csAPIURL, containerID, err := runImage(dockerClient, volumeName, branchToImageID, lastVer)
	if err != nil {
		return fmt.Errorf("failed to run container for branch %v: %v", lastVer, err)
	}
	defer destroyContainer(dockerClient, containerID)
	return verifyTestsRan(csAPIURL, versions)
}

// cleanup old containers/volumes from a previous run
func cleanup(dockerClient *client.Client) {
	// ignore all errors, we are just cleaning up and don't want to fail just because we fail to cleanup
	containers, _ := dockerClient.ContainerList(context.Background(), container.ListOptions{
		Filters: label(dendriteUpgradeTestLabel),
		All:     true,
	})
	for _, c := range containers {
		log.Printf("Removing container: %v %v\n", c.ID, c.Names)
		timeout := 1
		_ = dockerClient.ContainerStop(context.Background(), c.ID, container.StopOptions{Timeout: &timeout})
		_ = dockerClient.ContainerRemove(context.Background(), c.ID, container.RemoveOptions{
			Force: true,
		})
	}
	_ = dockerClient.VolumeRemove(context.Background(), "dendrite_upgrade_test", true)
}

func label(in string) filters.Args {
	f := filters.NewArgs()
	f.Add("label", in)
	return f
}

func main() {
	flag.Parse()
	httpClient := &http.Client{
		Timeout: 60 * time.Second,
	}
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatalf("failed to make docker client: %s", err)
	}
	if *flagFrom == "" {
		flag.Usage()
		os.Exit(1)
	}
	cleanup(dockerClient)
	versions := calculateVersions(httpClient, *flagFrom, *flagTo, *flagRepository, *flagDirect)
	log.Printf("Testing dendrite versions: %v\n", versions)

	branchToImageID := buildDendriteImages(httpClient, dockerClient, *flagTempDir, *flagRepository, *flagBuildConcurrency, versions)

	// make a shared postgres volume
	volume, err := dockerClient.VolumeCreate(context.Background(), volume.CreateOptions{
		Name: "dendrite_upgrade_test",
		Labels: map[string]string{
			dendriteUpgradeTestLabel: "yes",
		},
	})
	if err != nil {
		log.Fatalf("failed to make docker volume: %s", err)
	}

	failed := false
	defer func() {
		perr := recover()
		log.Println("removing postgres volume")
		verr := dockerClient.VolumeRemove(context.Background(), volume.Name, true)
		if perr == nil {
			perr = verr
		}
		if perr != nil {
			panic(perr)
		}
		if failed {
			os.Exit(1)
		}
	}()

	// run through images sequentially
	for _, v := range versions {
		if err = loadAndRunTests(dockerClient, volume.Name, v, branchToImageID); err != nil {
			log.Printf("failed to run tests for %v: %s\n", v, err)
			failed = true
			break
		}
	}
	if err := verifyTests(dockerClient, volume.Name, versions, branchToImageID); err != nil {
		log.Printf("failed to verify test results: %s", err)
		failed = true
	}
}
