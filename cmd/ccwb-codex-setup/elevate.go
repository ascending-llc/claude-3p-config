package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type (
	installSteps struct {
		requirements bool
		awsCLI       bool
	}

	pathACL struct {
		owner     string
		rules     []aclRule
		protected bool
	}

	aclRule struct {
		sid         string
		inheritance string
		rights      int
		allow       bool
		inherited   bool
	}
)

const (
	awsCLISigner        = "Amazon Web Services, Inc."
	administratorSID    = "S-1-5-32-544"
	systemSID           = "S-1-5-18"
	usersSID            = "S-1-5-32-545"
	trustedInstallerSID = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
	fullControlMask     = 2032127
	usersReadMask       = 1179817
	exitElevatedConfig  = 10
	exitDownload        = 11
	exitSignature       = 12
	exitInstall         = 13
	exitPermissions     = 14
	exitWrite           = 15
	exitUACCancelled    = 1223
)

var (
	awsVersionPrefix = regexp.MustCompile(`^aws-cli/([0-9]+)\.`)
	sidPattern       = regexp.MustCompile(`^S-[0-9]+(?:-[0-9]+)+$`)
)

func parseInstallSteps(value string) (installSteps, error) {
	switch value {
	case "requirements":
		return installSteps{requirements: true}, nil
	case "awscli":
		return installSteps{awsCLI: true}, nil
	case "requirements,awscli":
		return installSteps{requirements: true, awsCLI: true}, nil
	default:
		return installSteps{}, fmt.Errorf("invalid installation steps %q", value)
	}
}

func (s installSteps) argument() string {
	switch {
	case s.requirements && s.awsCLI:
		return "requirements,awscli"
	case s.requirements:
		return "requirements"
	case s.awsCLI:
		return "awscli"
	default:
		return ""
	}
}

// remainingSteps checks only requested work, preserving a user's choice to keep
// an existing configuration even when it differs from the built-in settings.
func remainingSteps(requested, ready installSteps) installSteps {
	return installSteps{
		requirements: requested.requirements && !ready.requirements,
		awsCLI:       requested.awsCLI && !ready.awsCLI,
	}
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func elevationScript(exe string, steps installSteps) string {
	return "try { $p = Start-Process -FilePath " + powershellQuote(exe) +
		" -ArgumentList '-elevated'," + powershellQuote(steps.argument()) +
		" -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ErrorAction Stop } catch { exit 1223 }; exit $p.ExitCode"
}

func parseAWSVersion(output []byte) (int, error) {
	match := awsVersionPrefix.FindStringSubmatch(strings.TrimSpace(string(output)))
	if match == nil {
		return 0, fmt.Errorf("the AWS CLI returned an unrecognized version")
	}

	major, err := strconv.Atoi(match[1])
	if err != nil || major < 1 {
		return 0, fmt.Errorf("the AWS CLI returned an invalid major version")
	}

	return major, nil
}

func validAWSSignature(output []byte) bool {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if strings.TrimSpace(lines[0]) != "Valid" {
		return false
	}

	organizations := 0

	for _, line := range lines[1:] {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "O=")
		if !found {
			continue
		}

		organizations++

		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}

		if value != awsCLISigner {
			return false
		}
	}

	return organizations == 1
}

func parseACL(output []byte) (pathACL, error) {
	var acl pathACL

	ownerSeen, protectedSeen := false, false

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return pathACL{}, fmt.Errorf("invalid permissions output")
		}

		switch key {
		case "owner":
			if ownerSeen || !sidPattern.MatchString(value) {
				return pathACL{}, fmt.Errorf("invalid owner output")
			}

			acl.owner, ownerSeen = value, true
		case "protected":
			if protectedSeen || (value != "True" && value != "False") {
				return pathACL{}, fmt.Errorf("invalid inheritance output")
			}

			acl.protected, protectedSeen = value == "True", true
		case "rule":
			rule, err := parseACLRule(value)
			if err != nil {
				return pathACL{}, err
			}

			acl.rules = append(acl.rules, rule)
		default:
			return pathACL{}, fmt.Errorf("unexpected permissions field %q", key)
		}
	}

	if !ownerSeen || !protectedSeen {
		return pathACL{}, fmt.Errorf("incomplete permissions output")
	}

	return acl, nil
}

func parseACLRule(value string) (aclRule, error) {
	permissions, flags, found := strings.Cut(value, " inh=")
	if !found {
		return aclRule{}, fmt.Errorf("missing rule inheritance flags")
	}

	inheritance, inherited, found := strings.Cut(flags, " inherited=")
	if !found || (inherited != "True" && inherited != "False") {
		return aclRule{}, fmt.Errorf("invalid rule inheritance output")
	}

	switch inheritance {
	case "ObjectInherit, ContainerInherit":
		inheritance = "ContainerInherit, ObjectInherit"
	case "None", "ContainerInherit", "ObjectInherit", "ContainerInherit, ObjectInherit":
	default:
		return aclRule{}, fmt.Errorf("invalid rule inheritance flags")
	}

	fields := strings.Fields(permissions)
	if len(fields) != 3 || !sidPattern.MatchString(fields[0]) || (fields[1] != "Allow" && fields[1] != "Deny") {
		return aclRule{}, fmt.Errorf("invalid access rule output")
	}

	rights, err := strconv.Atoi(fields[2])
	if err != nil {
		return aclRule{}, fmt.Errorf("invalid access rule rights")
	}

	return aclRule{
		sid: fields[0], allow: fields[1] == "Allow", rights: rights,
		inheritance: inheritance, inherited: inherited == "True",
	}, nil
}

func (acl pathACL) directoryLocked() bool {
	return acl.owner == administratorSID && acl.protected && acl.matchesGrants(false, "ContainerInherit, ObjectInherit")
}

// fileLocked requires the file to inherit only the locked directory's grants.
// A separate explicit grant or an untrusted owner could allow changes.
func (acl pathACL) fileLocked() bool {
	return trustedOwner(acl.owner) && !acl.protected && acl.matchesGrants(true, "None")
}

func trustedOwner(sid string) bool {
	return sid == administratorSID || sid == systemSID || sid == trustedInstallerSID
}

func (acl pathACL) matchesGrants(inherited bool, inheritance string) bool {
	if len(acl.rules) != 3 {
		return false
	}

	wanted := map[string]int{
		systemSID: fullControlMask, administratorSID: fullControlMask, usersSID: usersReadMask,
	}

	for _, rule := range acl.rules {
		rights, found := wanted[rule.sid]
		if !found || !rule.allow || rule.rights != rights || rule.inherited != inherited {
			return false
		}

		if rule.inheritance != inheritance {
			return false
		}

		delete(wanted, rule.sid)
	}

	return len(wanted) == 0
}

func childFailureMessage(code int) string {
	switch code {
	case 0:
		return ""
	case exitElevatedConfig:
		return "The administrator step received invalid tasks or built-in settings."
	case exitDownload:
		return "The AWS CLI download failed."
	case exitSignature:
		return "The AWS CLI installer signature could not be verified."
	case exitInstall:
		return "The AWS CLI installation failed."
	case exitPermissions:
		return "The Codex settings file or folder could not be created or secured."
	case exitWrite:
		return "The Codex settings file could not be written."
	case exitUACCancelled:
		return "The administrator prompt was declined or could not be opened."
	default:
		return fmt.Sprintf("The administrator step could not run (exit code %d).", code)
	}
}
