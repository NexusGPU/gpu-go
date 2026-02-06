package agent

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Agent Register", func() {
	It("returns ErrAlreadyRegistered when config exists", func() {
		tempDir := GinkgoT().TempDir()
		configDir := filepath.Join(tempDir, "config")
		stateDir := filepath.Join(tempDir, "state")
		configMgr := config.NewManager(configDir, stateDir)

		err := configMgr.SaveConfig(&config.Config{
			ConfigVersion: 1,
			AgentID:       "agent_existing",
			AgentSecret:   "gpugo_secret",
		})
		Expect(err).NotTo(HaveOccurred())

		var callCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&callCount, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := api.NewClient(api.WithBaseURL(server.URL))
		agentInstance := NewAgent(client, configMgr)

		err = agentInstance.Register("tmp_token", nil)
		Expect(err).To(MatchError(ErrAlreadyRegistered))
		Expect(atomic.LoadInt32(&callCount)).To(Equal(int32(0)))
	})
})
