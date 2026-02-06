//go:build darwin

package studio

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Apple container parsing", func() {
	It("maps container list JSON to Environment and SSH port", func() {
		output := []byte(`[
  {
    "status": "running",
    "configuration": {
      "id": "ggo-test-env-1234",
      "image": {"reference": "alpine:latest"},
      "labels": {
        "ggo.managed": "true",
        "ggo.name": "test-env",
        "ggo.mode": "apple-container"
      },
      "initProcess": {
        "environment": [
          "TENSOR_FUSION_OPERATOR_CONNECTION_INFO=https://worker.example.com:9001",
          "FOO=bar"
        ]
      },
      "publishedPorts": [
        {"hostPort": 18022, "containerPort": 22, "proto": "tcp", "count": 1}
      ]
    },
    "networks": []
  }
]`)

		envs, err := parseAppleContainerList(output)
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(HaveLen(1))

		env := envs[0]
		Expect(env.Name).To(Equal("test-env"))
		Expect(env.Image).To(Equal("alpine:latest"))
		Expect(env.Status).To(Equal(StatusRunning))
		Expect(env.SSHPort).To(Equal(18022))
		Expect(env.GPUWorkerURL).To(Equal("https://worker.example.com:9001"))
	})

	It("filters out containers without ggo.managed label", func() {
		output := []byte(`[
  {
    "status": "running",
    "configuration": {
      "id": "ggo-managed",
      "image": {"reference": "alpine:latest"},
      "labels": {"ggo.managed": "true"},
      "initProcess": {"environment": []},
      "publishedPorts": []
    },
    "networks": []
  },
  {
    "status": "running",
    "configuration": {
      "id": "user-container",
      "image": {"reference": "alpine:latest"},
      "labels": {"owner": "user"},
      "initProcess": {"environment": []},
      "publishedPorts": []
    },
    "networks": []
  }
]`)

		envs, err := parseAppleContainerList(output)
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(HaveLen(1))
		Expect(envs[0].ID).To(Equal("ggo-managed"))
	})
})
