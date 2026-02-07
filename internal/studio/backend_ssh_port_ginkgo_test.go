package studio

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SSH port parsing", func() {
	Describe("parseSSHPort", func() {
		It("returns mapped host SSH port when 22/tcp is published", func() {
			port := parseSSHPort("0.0.0.0:15432->22/tcp")
			Expect(port).To(Equal(15432))
		})

		It("returns zero when no SSH port mapping exists", func() {
			Expect(parseSSHPort("0.0.0.0:8888->8888/tcp")).To(BeZero())
			Expect(parseSSHPort("")).To(BeZero())
		})
	})

	Describe("extractSSHPort", func() {
		It("returns mapped host SSH port for Apple Container", func() {
			port := extractSSHPort([]appleContainerPublishedPort{{HostPort: 17000, ContainerPort: 22, Proto: "tcp", Count: 1}})
			Expect(port).To(Equal(17000))
		})

		It("returns zero when Apple Container has no SSH mapping", func() {
			port := extractSSHPort([]appleContainerPublishedPort{{HostPort: 8080, ContainerPort: 8080, Proto: "tcp", Count: 1}})
			Expect(port).To(BeZero())
		})
	})
})
