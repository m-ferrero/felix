// Copyright (c) 2016 Tigera, Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package iptables

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"strings"
)

const (
	// Compromise: shorter is better for table occupancy and readability. Longer is better for
	// collision-resistance.  16 chars gives us 96 bits of entropy, which is fairly collision
	// resistant.
	HashLength = 16
)

type Action interface {
	ToFragment() string
}

type GotoAction struct {
	Target string
}

func (g GotoAction) ToFragment() string {
	return "--goto " + g.Target
}

type JumpAction struct {
	Target string
}

func (g JumpAction) ToFragment() string {
	return "--jump " + g.Target
}

type ReturnAction struct{}

func (r ReturnAction) ToFragment() string {
	return "--jump RETURN"
}

type DropAction struct{}

func (g DropAction) ToFragment() string {
	return "--jump DROP"
}

type AcceptAction struct{}

func (g AcceptAction) ToFragment() string {
	return "--jump ACCEPT"
}

type DNATAction struct {
	DestAddr string
	DestPort uint16
}

func (g DNATAction) ToFragment() string {
	return fmt.Sprintf("--jump DNAT --to-destination %s:%d", g.DestAddr, g.DestPort)
}

type MasqAction struct{}

func (g MasqAction) ToFragment() string {
	return "--jump MASQUERADE"
}

type ClearMarkAction struct {
	Mark uint32
}

func (c ClearMarkAction) ToFragment() string {
	return fmt.Sprintf("--jump MARK --set-mark 0/%x", c.Mark)
}

type SetMarkAction struct {
	Mark uint32
}

func (c SetMarkAction) ToFragment() string {
	return fmt.Sprintf("--jump MARK --set-mark %x/%x", c.Mark, c.Mark)
}

type Rule struct {
	Match   MatchCriteria
	Action  Action
	Comment string
}

func (r Rule) RenderAppend(chainName, prefixFragment string) string {
	fragments := make([]string, 0, 6)
	fragments = append(fragments, "-A", chainName)
	return r.renderInner(fragments, prefixFragment)
}

func (r Rule) RenderInsert(chainName, prefixFragment string) string {
	fragments := make([]string, 0, 6)
	fragments = append(fragments, "-I", chainName)
	return r.renderInner(fragments, prefixFragment)
}

func (r Rule) RenderReplace(chainName string, ruleNum int, prefixFragment string) string {
	fragments := make([]string, 0, 7)
	fragments = append(fragments, "-R", chainName, fmt.Sprintf("%d", ruleNum))
	return r.renderInner(fragments, prefixFragment)
}

func (r Rule) renderInner(fragments []string, prefixFragment string) string {
	if prefixFragment != "" {
		fragments = append(fragments, prefixFragment)
	}
	if r.Comment != "" {
		commentFragment := fmt.Sprintf("-m comment --comment \"%s\"", r.Comment)
		fragments = append(fragments, commentFragment)
	}
	matchFragment := r.Match.Render()
	if matchFragment != "" {
		fragments = append(fragments, matchFragment)
	}
	actionFragment := r.Action.ToFragment()
	if actionFragment != "" {
		fragments = append(fragments, actionFragment)
	}
	return strings.Join(fragments, " ")
}

type Chain struct {
	Name  string
	Rules []Rule
}

func (c *Chain) RuleHashes() []string {
	hashes := make([]string, len(c.Rules))
	// First hash the chain name so that identical rules in different chains will get different
	// hashes.
	s := sha256.New224()
	s.Write([]byte(c.Name))
	hash := s.Sum(nil)
	for ii, rule := range c.Rules {
		// Each hash chains in the previous hash, so that its position in the chain and
		// the rules before it affect its hash.
		s.Reset()
		s.Write(hash)
		ruleForHashing := rule.RenderAppend(c.Name, "HASH")
		s.Write([]byte(ruleForHashing))
		hash = s.Sum(hash[0:0])
		// Encode the hash using a compact character set.  We use the URL-safe base64
		// variant because it uses '-' and '_', which are more shell-friendly.
		hashes[ii] = base64.RawURLEncoding.EncodeToString(hash)[:HashLength]
		if log.GetLevel() >= log.DebugLevel {
			log.WithFields(log.Fields{
				"ruleFragment": ruleForHashing,
				"action":       rule.Action,
				"position":     ii,
				"chain":        c.Name,
				"hash":         hashes[ii],
			}).Debug("Hashed rule")
		}
	}
	return hashes
}