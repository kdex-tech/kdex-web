/*
Copyright 2025.

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
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert/yaml"
	"golang.org/x/mod/modfile"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kdexv1alpha1 "kdex.dev/crds/api/v1alpha1"
	"kdex.dev/web/internal/store"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	focalHost string
	namespace string
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	// Get the default Ginkgo configuration
	suiteConfig, reporterConfig := GinkgoConfiguration()

	// Enable full stack traces
	reporterConfig.FullTrace = true
	RunSpecs(t, "Controller Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{}, // No local CRDs initially
		ErrorIfCRDPathMissing: false,
	}

	tempDir, err := os.MkdirTemp("", "crd")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir)

	kdexCrdVersion := getCRDModuleVersion()
	array := fetchSetofCRDs(kdexCrdVersion, tempDir)

	for _, path := range array {
		fullPath := fmt.Sprintf("https://raw.githubusercontent.com/kdex-tech/kdex-crds/refs/tags/%s/config/crd/%s", kdexCrdVersion, path)
		addRemoteCRD(&testEnv.CRDDirectoryPaths, tempDir, fullPath)
	}

	addRemoteCRD(&testEnv.CRDDirectoryPaths, tempDir, "https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml")

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = kdexv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = gatewayv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	focalHost = "test-host"
	namespace = "default"

	k8sManager, err := manager.New(cfg, manager.Options{
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				namespace: {},
			},
		},
		Scheme: scheme.Scheme,
	})
	Expect(err).NotTo(HaveOccurred())

	hostStore := store.NewHostStore()

	hostReconciler := &KDexHostReconciler{
		Client:              k8sManager.GetClient(),
		ControllerNamespace: namespace,
		Defaults:            Defaults("/config.yaml"),
		FocalHost:           focalHost,
		HostStore:           hostStore,
		Port:                8090,
		RequeueDelay:        0,
		Scheme:              k8sManager.GetScheme(),
	}

	err = hostReconciler.SetupWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	renderPageReconciler := &KDexRenderPageReconciler{
		Client:       k8sClient,
		HostStore:    hostStore,
		RequeueDelay: 0,
		Scheme:       k8sClient.Scheme(),
	}

	err = renderPageReconciler.SetupWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	translationReconciler := &KDexTranslationReconciler{
		Client:       k8sClient,
		HostStore:    hostStore,
		RequeueDelay: 0,
		Scheme:       k8sClient.Scheme(),
	}

	err = translationReconciler.SetupWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func addRemoteCRD(paths *[]string, tempDir string, url string) {
	crdPath, err := downloadCRD(url, tempDir)
	if err != nil {
		panic(err)
	}

	*paths = append(*paths, crdPath)
}

func assertResourceReady(ctx context.Context, k8sClient client.Client, name string, namespace string, checkResource client.Object, ready bool) {
	typeNamespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	check := func(g Gomega) {
		err := k8sClient.Get(ctx, typeNamespacedName, checkResource)
		g.Expect(err).NotTo(HaveOccurred())
		it := reflect.ValueOf(checkResource).Elem()
		statusField := it.FieldByName("Status")
		g.Expect(statusField.IsValid()).To(BeTrue())
		conditionsField := statusField.FieldByName("Conditions")
		g.Expect(conditionsField.IsValid()).To(BeTrue())
		conditions, ok := conditionsField.Interface().([]metav1.Condition)
		g.Expect(ok).To(BeTrue())
		if ready {
			g.Expect(
				apimeta.IsStatusConditionTrue(
					conditions, string(kdexv1alpha1.ConditionTypeReady),
				),
			).To(BeTrue())
		} else {
			g.Expect(
				apimeta.IsStatusConditionFalse(
					conditions, string(kdexv1alpha1.ConditionTypeReady),
				),
			).To(BeTrue())
		}
	}

	Eventually(check).Should(Succeed())
}

func downloadCRD(url, tempDir string) (string, error) {
	httpClient := &http.Client{}
	response, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download CRD from %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download CRD from %s: status code %d", url, response.StatusCode)
	}

	crdContent, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read CRD content from %s: %w", url, err)
	}

	fileName := filepath.Base(url)
	filePath := filepath.Join(tempDir, fileName)
	err = os.WriteFile(filePath, crdContent, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to write CRD to file %s: %w", filePath, err)
	}

	return filePath, nil
}

func getCRDModuleVersion() string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(GinkgoWriter, "Current working directory: %s\n", cwd)

	modBytes, err := os.ReadFile("../../go.mod")
	if err != nil {
		panic(err)
	}

	modFile, err := modfile.Parse("go.mod", modBytes, nil)
	if err != nil {
		panic(err)
	}

	for _, r := range modFile.Replace {
		fmt.Printf("  Old: %s => New: %s %s\n", r.Old.Path, r.New.Path, r.New.Version)
		if r.Old.Path == "kdex.dev/crds" {
			return r.New.Version
		}
	}

	panic("Couldn't find kdex-crds in go.mod")
}

func fetchSetofCRDs(kdexCrdVersion string, tempDir string) []string {
	yamlURL := fmt.Sprintf("https://raw.githubusercontent.com/kdex-tech/kdex-crds/refs/tags/%s/config/crd/kustomization.yaml", kdexCrdVersion)
	req, err := http.NewRequest("GET", yamlURL, nil)
	if err != nil {
		panic(err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	yamlObject := struct {
		Resources []string `yaml:"resources"`
	}{}

	err = yaml.Unmarshal(body, &yamlObject)
	if err != nil {
		panic(err)
	}

	return yamlObject.Resources
}

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
