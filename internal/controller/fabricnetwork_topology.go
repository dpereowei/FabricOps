/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"strings"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

func validateFabricNetworkTopology(net *fabricopsv1alpha1.FabricNetwork) []string {
	problems := []string{}
	orgs := map[string]fabricopsv1alpha1.Org{}
	ordererCount := 0

	if len(net.Spec.Orgs) == 0 {
		problems = append(problems, "spec.orgs must include at least one organization")
	}

	for _, org := range net.Spec.Orgs {
		orgName := strings.TrimSpace(org.Organization.Name)
		if orgName == "" {
			continue
		}
		if _, ok := orgs[orgName]; ok {
			problems = append(problems, fmt.Sprintf("org %q is declared more than once", orgName))
		}
		orgs[orgName] = org

		for _, group := range org.Orderers {
			ordererCount += group.Instances
		}
	}

	if ordererCount == 0 {
		problems = append(problems, "at least one orderer instance is required")
	}
	if len(net.Spec.Channels) > 0 && !net.Spec.Global.TLS {
		problems = append(problems, "spec.global.tls must be true when channels are declared")
	}

	channels := map[string]fabricopsv1alpha1.Channel{}
	for _, channel := range net.Spec.Channels {
		channelName := strings.TrimSpace(channel.Name)
		if channelName == "" {
			continue
		}
		if _, ok := channels[channelName]; ok {
			problems = append(problems, fmt.Sprintf("channel %q is declared more than once", channelName))
		}
		channels[channelName] = channel

		if len(channel.Orgs) == 0 {
			problems = append(problems, fmt.Sprintf("channel %q must include at least one peer org", channelName))
		}
		for _, channelOrg := range channel.Orgs {
			orgName := strings.TrimSpace(channelOrg.Name)
			org, ok := orgs[orgName]
			if !ok {
				problems = append(problems, fmt.Sprintf("channel %q references unknown org %q", channelName, orgName))
				continue
			}
			if org.Peer == nil || org.Peer.Instances == 0 {
				problems = append(problems, fmt.Sprintf("channel %q org %q has no peer instances", channelName, orgName))
				continue
			}
			if len(channelOrg.Peers) == 0 {
				problems = append(problems, fmt.Sprintf("channel %q org %q must include at least one peer", channelName, orgName))
				continue
			}
			if unknownPeers := unknownChannelPeers(org, channelOrg.Peers); len(unknownPeers) > 0 {
				problems = append(
					problems,
					fmt.Sprintf("channel %q org %q references unknown peers: %s", channelName, orgName, strings.Join(unknownPeers, ", ")),
				)
			}
		}
	}

	seenChaincodes := map[string]struct{}{}
	for _, chaincode := range net.Spec.Chaincodes {
		chaincodeName := strings.TrimSpace(chaincode.Name)
		channelName := strings.TrimSpace(chaincode.Channel)
		if chaincodeName == "" || channelName == "" {
			continue
		}

		key := channelName + "/" + chaincodeName
		if _, ok := seenChaincodes[key]; ok {
			problems = append(problems, fmt.Sprintf("chaincode %q is declared more than once on channel %q", chaincodeName, channelName))
		}
		seenChaincodes[key] = struct{}{}

		if _, ok := channels[channelName]; !ok {
			problems = append(problems, fmt.Sprintf("chaincode %q references unknown channel %q", chaincodeName, channelName))
		}
	}

	return problems
}
