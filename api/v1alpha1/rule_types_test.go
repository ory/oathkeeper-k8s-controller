/*

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

package v1alpha1

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// These tests are written in BDD-style using Ginkgo framework. Refer to
// http://onsi.github.io/ginkgo to learn more.

var _ = Describe("Rule", func() {
	var (
		key              types.NamespacedName
		created, fetched *Rule
	)

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	// Add Tests for OpenAPI validation (or additonal CRD features) specified in
	// your API definition.
	// Avoid adding tests for vanilla CRUD operations because they would
	// test Kubernetes API server, which isn't the goal here.
	Context("Create API", func() {

		It("should create an object successfully", func() {

			key = types.NamespacedName{
				Name:      "sample-rule1",
				Namespace: "default",
			}

			h := newHandler("sample-handler", "{}")

			created = newRule(
				"sample-rule1",
				"default",
				"https://url.com",
				"https://url2.com",
				nil,
				newBoolPtr(true),
				[]*Authenticator{&Authenticator{h}},
				&Authorizer{h},
				&Mutator{h})

			By("creating an API obj")
			Expect(k8sClient.Create(context.TODO(), created)).To(Succeed())

			fetched = &Rule{}
			Expect(k8sClient.Get(context.TODO(), key, fetched)).To(Succeed())
			Expect(fetched).To(Equal(created))

			By("deleting the created object")
			Expect(k8sClient.Delete(context.TODO(), created)).To(Succeed())
			Expect(k8sClient.Get(context.TODO(), key, created)).ToNot(Succeed())
		})
	})

	var template = `[
  {
    "upstream": {
      "url": "http://my-backend-service1",
      "strip_path": "/api/v1",
      "preserve_host": true
    },
    "id": "foo1.default",
    "match": {
      "url": "http://my-app/some-route1",
      "methods": [
        "GET",
        "POST"
      ]
    },
    "authenticators": [
      {
        "handler": "handler1",
        "config": {
          "key1": "val1"
        }
      }
    ],
    "authorizer": {
      "handler": "allow"
    },
    "mutator": {
      "handler": "handler2",
      "config": {
        "key1": [
          "val1",
          "val2",
          "val3"
        ]
      }
    }
  },
  {
    "upstream": {
      "url": "http://my-backend-service2",
      "preserve_host": false
    },
    "id": "foo2.default",
    "match": {
      "url": "http://my-app/some-route2",
      "methods": [
        "GET",
        "POST"
      ]
    },
    "authenticators": [
      {
        "handler": "handler1",
        "config": {
          "key1": "val1"
        }
      },
      {
        "handler": "handler2",
        "config": {
          "key1": [
            "val1",
            "val2",
            "val3"
          ]
        }
      }
    ],
    "authorizer": {
      "handler": "allow"
    },
    "mutator": {
      "handler": "noop"
    }
  },
  {
    "upstream": {
      "url": "http://my-backend-service3",
      "preserve_host": false
    },
    "id": "foo3.default",
    "match": {
      "url": "http://my-app/some-route3",
      "methods": [
        "GET",
        "POST"
      ]
    },
    "authenticators": [
      {
        "handler": "noop"
      }
    ],
    "authorizer": {
      "handler": "handler1",
      "config": {
        "key1": "val1"
      }
    },
    "mutator": {
      "handler": "noop"
    }
  }
]`

	var sampleConfig = `{
  "key1": "val1"
}
`

	var sampleConfig2 = `{
  "key1": [
    "val1",
    "val2",
    "val3"
  ]
}
`

	Context("ToOathkeeperRules", func() {

		It("Should return a JSON array of raw Oathkeeper rules", func() {

			h1 := newHandler("handler1", sampleConfig)
			h2 := newHandler("handler2", sampleConfig2)

			rule1 := newRule(
				"foo1",
				"default",
				"http://my-backend-service1",
				"http://my-app/some-route1",
				newStringPtr("/api/v1"),
				newBoolPtr(true),
				[]*Authenticator{&Authenticator{h1}},
				nil,
				&Mutator{h2})

			rule2 := newRule(
				"foo2",
				"default",
				"http://my-backend-service2",
				"http://my-app/some-route2",
				nil,
				newBoolPtr(false),
				[]*Authenticator{&Authenticator{h1}, {h2}},
				nil,
				nil)

			rule3 := newRule(
				"foo3",
				"default",
				"http://my-backend-service3",
				"http://my-app/some-route3",
				nil,
				nil,
				nil,
				&Authorizer{h1},
				nil)

			list := &RuleList{Items: []Rule{*rule1, *rule2, *rule3}}

			By("transforming the receiver into a slice of bytes")

			raw, err := list.ToOathkeeperRules()

			Expect(err).To(BeNil())
			Expect(string(raw)).To(Equal(template))
		})
	})

	Context("ToRuleJSON", func() {

		It("Should convert a Rule with no handlers to JSON Rule", func() {

			testRule := newRule(
				"r1",
				"test",
				"https://upstream.url",
				"https://match.this/url",
				newStringPtr("/strip/me"),
				nil,
				nil,
				nil,
				nil)

			actual := testRule.ToRuleJSON()

			By("copying its spec, adding default handlers and generating correct item ID")

			Expect(actual.ID).To(Equal("r1.test"))

			Expect(actual.RuleSpec.Authenticators).NotTo(BeNil())
			Expect(actual.RuleSpec.Authenticators).NotTo(BeEmpty())
			Expect(actual.RuleSpec.Authenticators).To(HaveLen(1))
			Expect(actual.RuleSpec.Authenticators[0].Handler).To(Equal(noopHandler))

			Expect(actual.RuleSpec.Authorizer).NotTo(BeNil())
			Expect(actual.RuleSpec.Authorizer.Handler).To(Equal(allowHandler))

			Expect(actual.RuleSpec.Mutator).NotTo(BeNil())
			Expect(actual.RuleSpec.Mutator.Handler).To(Equal(noopHandler))

			Expect(*actual.RuleSpec.Upstream.PreserveHost).To(BeFalse())
		})

		It("Should convert a Rule with specified handlers to JSON Rule", func() {

			testHandler := newHandler("test-handler", "")

			testRule := newRule(
				"r1",
				"test",
				"https://upstream.url",
				"https://match.this/url",
				newStringPtr("/strip/me"),
				newBoolPtr(true),
				[]*Authenticator{&Authenticator{testHandler}},
				&Authorizer{testHandler},
				&Mutator{testHandler})

			actual := testRule.ToRuleJSON()

			By("copying its spec and generating correct item ID")

			Expect(actual.ID).To(Equal("r1.test"))
			Expect(actual.RuleSpec).To(Equal(testRule.Spec))
		})
	})
})

func newRule(name, namespace, upstreamURL, matchURL string, stripURLPath *string, preserveURLHost *bool, authenticators []*Authenticator, authorizer *Authorizer, mutator *Mutator) *Rule {

	spec := RuleSpec{
		Upstream: &Upstream{
			URL:          upstreamURL,
			PreserveHost: preserveURLHost,
			StripPath:    stripURLPath,
		},
		Match: &Match{
			URL:     matchURL,
			Methods: []string{"GET", "POST"},
		},
		Authenticators: authenticators,
		Authorizer:     authorizer,
		Mutator:        mutator,
	}

	return &Rule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

func newHandler(name string, config string) *Handler {

	h := &Handler{
		Name: name,
	}

	if config != "" {
		h.Config = &runtime.RawExtension{
			Raw: []byte(config),
		}
	}

	return h
}

func newBoolPtr(b bool) *bool {
	return &b
}

func newStringPtr(s string) *string {
	return &s
}
