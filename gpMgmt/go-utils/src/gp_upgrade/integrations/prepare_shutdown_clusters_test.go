package integrations_test

import (
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"gp_upgrade/hub/cluster"
	"gp_upgrade/hub/configutils"
	"gp_upgrade/hub/services"
	"gp_upgrade/testutils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

var _ = Describe("prepare shutdown-clusters", func() {
	var (
		dir           string
		hub           *services.HubClient
		commandExecer *testutils.FakeCommandExecer
	)

	BeforeEach(func() {
		var err error
		dir, err = ioutil.TempDir("", "")
		Expect(err).ToNot(HaveOccurred())

		config := `[{
  			  "content": 2,
  			  "dbid": 7,
  			  "hostname": "localhost"
  			}]`

		testutils.WriteProvidedConfig(dir, config)

		conf := &services.HubConfig{
			CliToHubPort:   7527,
			HubToAgentPort: 6416,
			StateDir:       dir,
		}
		reader := configutils.NewReader()

		commandExecer = &testutils.FakeCommandExecer{}
		commandExecer.SetOutput(&testutils.FakeCommand{})

		hub = services.NewHub(&cluster.Pair{}, &reader, grpc.DialContext, commandExecer.Exec, conf)

		Expect(checkPortIsAvailable(7527)).To(BeTrue())
		go hub.Start()
	})

	AfterEach(func() {
		hub.Stop()
		Expect(checkPortIsAvailable(7527)).To(BeTrue())
		os.RemoveAll(dir)
	})

	It("updates status PENDING to RUNNING then to COMPLETE if successful", func(done Done) {
		defer close(done)
		oldBinDir, err := ioutil.TempDir("", "")
		Expect(err).ToNot(HaveOccurred())
		newBinDir, err := ioutil.TempDir("", "")
		Expect(err).ToNot(HaveOccurred())

		Expect(runStatusUpgrade()).To(ContainSubstring("PENDING - Shutdown clusters"))

		trigger := make(chan struct{}, 1)
		commandExecer.SetTrigger(trigger)

		wg := &sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()

			Eventually(runStatusUpgrade).Should(ContainSubstring("RUNNING - Shutdown clusters"))
			trigger <- struct{}{}
		}()

		prepareShutdownClustersSession := runCommand("prepare", "shutdown-clusters", "--old-bindir", oldBinDir, "--new-bindir", newBinDir)
		Eventually(prepareShutdownClustersSession).Should(Exit(0))
		wg.Wait()

		Expect(commandExecer.Command()).To(Equal("bash"))
		Expect(strings.Join(commandExecer.Args(), "")).To(ContainSubstring("gpstop"))
		Eventually(runStatusUpgrade).Should(ContainSubstring("COMPLETE - Shutdown clusters"))
	})

	It("updates status to FAILED if it fails to run", func() {
		oldBinDir, err := ioutil.TempDir("", "")
		Expect(err).ToNot(HaveOccurred())
		newBinDir, err := ioutil.TempDir("", "")
		Expect(err).ToNot(HaveOccurred())

		Expect(runStatusUpgrade()).To(ContainSubstring("PENDING - Shutdown clusters"))

		commandExecer.SetOutput(&testutils.FakeCommand{
			Err: errors.New("start failed"),
			Out: nil,
		})

		prepareShutdownClustersSession := runCommand("prepare", "shutdown-clusters", "--old-bindir", oldBinDir, "--new-bindir", newBinDir)
		Eventually(prepareShutdownClustersSession).Should(Exit(1))

		Expect(commandExecer.Command()).To(Equal("bash"))
		Expect(strings.Join(commandExecer.Args(), "")).To(ContainSubstring("gpstop"))
		Eventually(runStatusUpgrade).Should(ContainSubstring("FAILED - Shutdown clusters"))
	})

	It("fails if the --old-bindir or --new-bindir flags are missing", func() {
		prepareShutdownClustersSession := runCommand("prepare", "shutdown-clusters")
		Expect(prepareShutdownClustersSession).Should(Exit(1))
		Expect(string(prepareShutdownClustersSession.Out.Contents())).To(Equal("Required flag(s) \"new-bindir\", \"old-bindir\" have/has not been set\n"))
	})
})
