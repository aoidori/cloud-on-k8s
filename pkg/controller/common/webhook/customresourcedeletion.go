// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package webhook

import (
	"context"
	"net/http"

	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	lsv1alpha1 "github.com/elastic/cloud-on-k8s/v2/pkg/apis/logstash/v1alpha1"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/k8s"
	ulog "github.com/elastic/cloud-on-k8s/v2/pkg/utils/log"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/set"
)

// +kubebuilder:webhook:path=/validate-prevent-crd-deletion-k8s-elastic-co,mutating=false,failurePolicy=ignore,groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=delete,versions=v1,name=elastic-prevent-crd-deletion.k8s.elastic.co,sideEffects=None,admissionReviewVersions=v1,matchPolicy=Exact

const (
	webhookPath = "/validate-prevent-crd-deletion-k8s-elastic-co"
)

var lslog = ulog.Log.WithName("crd-delete-validation")

// RegisterCRDDeletionWebhook will register the crd deletion prevention webhook.
func RegisterCRDDeletionWebhook(mgr ctrl.Manager, managedNamespace []string) {
	wh := &crdDeletionWebhook{
		client:           mgr.GetClient(),
		decoder:          admission.NewDecoder(mgr.GetScheme()),
		managedNamespace: set.Make(managedNamespace...),
	}
	lslog.Info("Registering CRD deletion prevention validating webhook", "path", webhookPath)
	mgr.GetWebhookServer().Register(webhookPath, &webhook.Admission{Handler: wh})
}

type crdDeletionWebhook struct {
	client           k8s.Client
	decoder          *admission.Decoder
	managedNamespace set.StringSet
}

func (wh *crdDeletionWebhook) ValidateCreate(ls *lsv1alpha1.Logstash) error {
	return nil
}

func (wh *crdDeletionWebhook) ValidateUpdate(ctx context.Context, prev *lsv1alpha1.Logstash, curr *lsv1alpha1.Logstash) error {
	return nil
}

// Handle is called when any request is sent to the webhook, satisfying the admission.Handler interface.
func (wh *crdDeletionWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	crd := &extensionsv1.CustomResourceDefinition{}
	err := wh.decoder.DecodeRaw(req.Object, crd)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if isElasticCRD(crd.GroupVersionKind()) && wh.isInUse(crd) {
		return admission.Denied("deletion of Elastic CRDs is not allowed")
	}

	return admission.Allowed("")
}

func isElasticCRD(gvk schema.GroupVersionKind) bool {
	return slices.Contains(
		[]string{
			"agent.k8s.elastic.co",
			"apm.k8s.elastic.co",
			"autoscaling.k8s.elastic.co",
			"beat.k8s.elastic.co",
			"elasticsearch.k8s.elastic.co",
			"enterprise-search.k8s.elastic.co",
			"kibana.k8s.elastic.co",
			"logstash.k8s.elastic.co",
			"maps.k8s.elastic.co",
			"stackconfigpolicy.k8s.elastic.co",
		}, gvk.Group)
}

func (wh *crdDeletionWebhook) isInUse(crd *extensionsv1.CustomResourceDefinition) bool {
	ul := &unstructured.UnstructuredList{}
	ul.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   crd.GroupVersionKind().Group,
		Kind:    crd.GroupVersionKind().Kind,
		Version: crd.GroupVersionKind().Version,
	})
	for _, ns := range wh.managedNamespace.AsSlice() {
		err := wh.client.List(context.Background(), ul, client.InNamespace(ns))
		if err != nil {
			lslog.Error(err, "Failed to list resources", "namespace", ns)
			return true
		}
		if len(ul.Items) > 0 {
			return true
		}
	}
	return false
}
