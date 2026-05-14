package frr

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// GenerateBFDConfig generates FRR bfdd configuration.
func GenerateBFDConfig(cfg *BFDConfig) (string, error) {
	if cfg == nil || (len(cfg.Profiles) == 0 && len(cfg.Peers) == 0) {
		return "", nil
	}
	if err := validateBFDConfig(cfg); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("bfd\n")

	profiles := append([]BFDProfile(nil), cfg.Profiles...)
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	for _, profile := range profiles {
		fmt.Fprintf(&b, " profile %s\n", profile.Name)
		writeBFDSessionCommands(&b, profile.DetectMultiplier, profile.ReceiveInterval, profile.TransmitInterval, profile.EchoMode, profile.PassiveMode)
		b.WriteString(" !\n")
	}

	peers := append([]BFDPeer(nil), cfg.Peers...)
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Address != peers[j].Address {
			return peers[i].Address < peers[j].Address
		}
		if peers[i].VRF != peers[j].VRF {
			return peers[i].VRF < peers[j].VRF
		}
		return peers[i].Interface < peers[j].Interface
	})
	for _, peer := range peers {
		fmt.Fprintf(&b, " peer %s", peer.Address)
		if peer.Interface != "" {
			fmt.Fprintf(&b, " interface %s", peer.Interface)
		}
		if peer.Multihop {
			b.WriteString(" multihop")
		}
		if peer.LocalAddress != "" {
			fmt.Fprintf(&b, " local-address %s", peer.LocalAddress)
		}
		if peer.VRF != "" && peer.VRF != "default" {
			fmt.Fprintf(&b, " vrf %s", peer.VRF)
		}
		b.WriteString("\n")
		if peer.Profile != "" {
			fmt.Fprintf(&b, "  profile %s\n", peer.Profile)
		}
		writeBFDSessionCommands(&b, peer.DetectMultiplier, peer.ReceiveInterval, peer.TransmitInterval, peer.EchoMode, peer.PassiveMode)
		if peer.Shutdown {
			b.WriteString("  shutdown\n")
		} else {
			b.WriteString("  no shutdown\n")
		}
		b.WriteString(" !\n")
	}

	b.WriteString("!\n")
	return b.String(), nil
}

func writeBFDSessionCommands(b *strings.Builder, detectMultiplier, receiveInterval, transmitInterval int, echoMode, passiveMode bool) {
	indent := "  "
	if receiveInterval != 0 {
		fmt.Fprintf(b, "%sreceive-interval %d\n", indent, receiveInterval)
	}
	if transmitInterval != 0 {
		fmt.Fprintf(b, "%stransmit-interval %d\n", indent, transmitInterval)
	}
	if detectMultiplier != 0 {
		fmt.Fprintf(b, "%sdetect-multiplier %d\n", indent, detectMultiplier)
	}
	if echoMode {
		fmt.Fprintf(b, "%secho-mode\n", indent)
	}
	if passiveMode {
		fmt.Fprintf(b, "%spassive-mode\n", indent)
	}
}

func validateBFDConfig(cfg *BFDConfig) error {
	profiles := make(map[string]BFDProfile, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if strings.TrimSpace(profile.Name) == "" {
			return fmt.Errorf("BFD profile name is required")
		}
		if _, ok := profiles[profile.Name]; ok {
			return fmt.Errorf("BFD profile %s is duplicated", profile.Name)
		}
		if err := validateBFDSessionTimers(fmt.Sprintf("BFD profile %s", profile.Name), profile.DetectMultiplier, profile.ReceiveInterval, profile.TransmitInterval); err != nil {
			return err
		}
		profiles[profile.Name] = profile
	}
	peers := make(map[string]struct{}, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		peerIP := net.ParseIP(peer.Address)
		if peerIP == nil {
			return fmt.Errorf("BFD peer has invalid address %q", peer.Address)
		}
		peerKey := peerIP.String()
		if _, ok := peers[peerKey]; ok {
			return fmt.Errorf("BFD peer %s is duplicated", peer.Address)
		}
		peers[peerKey] = struct{}{}
		if peer.LocalAddress != "" && net.ParseIP(peer.LocalAddress) == nil {
			return fmt.Errorf("BFD peer %s has invalid local-address %q", peer.Address, peer.LocalAddress)
		}
		if peer.Profile != "" {
			profile, ok := profiles[peer.Profile]
			if !ok {
				return fmt.Errorf("BFD peer %s references unknown profile %q", peer.Address, peer.Profile)
			}
			if peer.Multihop && profile.EchoMode {
				return fmt.Errorf("BFD peer %s cannot use echo-mode profile %q with multihop", peer.Address, peer.Profile)
			}
		}
		if peer.Multihop && peer.EchoMode {
			return fmt.Errorf("BFD peer %s cannot use echo-mode with multihop", peer.Address)
		}
		if err := validateBFDSessionTimers(fmt.Sprintf("BFD peer %s", peer.Address), peer.DetectMultiplier, peer.ReceiveInterval, peer.TransmitInterval); err != nil {
			return err
		}
	}
	return nil
}

func validateBFDSessionTimers(context string, detectMultiplier, receiveInterval, transmitInterval int) error {
	if detectMultiplier < 0 || detectMultiplier > 255 || detectMultiplier == 1 {
		return fmt.Errorf("%s detect-multiplier must be omitted or 2-255, got %d", context, detectMultiplier)
	}
	if receiveInterval < 0 || receiveInterval > 60000 || (receiveInterval > 0 && receiveInterval < 10) {
		return fmt.Errorf("%s receive-interval must be omitted or 10-60000, got %d", context, receiveInterval)
	}
	if transmitInterval < 0 || transmitInterval > 60000 || (transmitInterval > 0 && transmitInterval < 10) {
		return fmt.Errorf("%s transmit-interval must be omitted or 10-60000, got %d", context, transmitInterval)
	}
	return nil
}
