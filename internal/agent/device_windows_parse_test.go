package agent

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Windows GPU detection parsing", func() {
	It("parses PowerShell JSON output and prefers NVIDIA", func() {
		output := `[{"Name":"Intel(R) UHD Graphics 770","DriverVersion":"31.0.101.1234"},{"Name":"NVIDIA GeForce RTX 4090","DriverVersion":"31.0.15.3770"}]`

		infos := parsePowerShellVideoControllersJSON(output)
		vendor, driver := pickWindowsVendor(infos)

		Expect(vendor).To(Equal(vendorNVIDIA))
		Expect(driver).To(Equal("31.0.15.3770"))
	})

	It("parses WMIC CSV output", func() {
		output := "Node,Name,DriverVersion\r\nMY-PC,NVIDIA GeForce RTX 4090,31.0.15.3770\r\n"

		infos := parseWMICVideoControllerCSV(output)
		vendor, driver := pickWindowsVendor(infos)

		Expect(vendor).To(Equal(vendorNVIDIA))
		Expect(driver).To(Equal("31.0.15.3770"))
	})

	It("matches NVIDIA driver versions using WMI suffix", func() {
		Expect(nvidiaDriverVersionsMatch("551.23", "31.0.15.5123")).To(BeTrue())
		Expect(nvidiaDriverVersionsMatch("551.23", "31.0.15.4999")).To(BeFalse())
	})

	It("parses NVIDIA-SMI driver version output", func() {
		output := "550.54.15\n550.54.15\n"

		Expect(parseNvidiaSMIDriverVersion(output)).To(Equal("550.54.15"))
	})

	It("normalizes NVIDIA driver major versions from multiple formats", func() {
		major, ok := nvidiaDriverMajorFromVersion("550.54.15")
		Expect(ok).To(BeTrue())
		Expect(major).To(Equal(550))

		major, ok = nvidiaDriverMajorFromVersion("535.104.05")
		Expect(ok).To(BeTrue())
		Expect(major).To(Equal(535))

		major, ok = nvidiaDriverMajorFromVersion("31.0.15.3770")
		Expect(ok).To(BeTrue())
		Expect(major).To(Equal(537))
	})

	It("flags outdated NVIDIA driver versions", func() {
		Expect(isNvidiaDriverOutdated("534.99")).To(BeTrue())
		Expect(isNvidiaDriverOutdated("535.12")).To(BeFalse())
		Expect(isNvidiaDriverOutdated("31.0.15.3770")).To(BeFalse())
	})
})
