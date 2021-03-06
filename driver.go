package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"time"
	"encoding/base64"
    "os"
	"k8s.io/apimachinery/pkg/api/resource"
	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/state"
	"github.com/rancher/wrangler/pkg/apply"
	"github.com/rancher/wrangler/pkg/objectset"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// "k8s.io/client-go/rest"
)

// Driver contains kubernetes-specific data to implement [drivers.Driver]
type Driver struct {
	*drivers.BaseDriver
	Userdata string
	Image    string
	kubernetesToken string
}

const (
	defaultUser  = "sles"
	defaultImage = "ghcr.io/william86370/rke2ink:systemd"
)

func base64decodefile(b64 string) error {

	dec, err := base64.StdEncoding.DecodeString(b64)
    if err != nil {
        return fmt.Errorf("Cannot Decode base64 file %v", err)
    }

    f, err := os.Create("/.kube/config")
	// os.Setenv("KUBECONFIG", "/.kube/config")
    if err != nil {
        return fmt.Errorf("cannot create file %v", err)
    }
    defer f.Close()

    if _, err := f.Write(dec); err != nil {
        panic(err)
    }
    if err := f.Sync(); err != nil {
        panic(err)
    }
	return nil
}

// NewDriver creates a Driver with the specified storePath.
func NewDriver(machineName string, storePath string) *Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			SSHUser:     defaultUser,
			SSHPort:     22,
			MachineName: machineName,
			StorePath:   storePath,
		},
	}
}

// DriverName returns the name of the driver
func (d *Driver) DriverName() string {
	return "cloudca"
}

// GetCreateFlags registers the flags this driver adds to
// "docker hosts create"
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			Name:   "cloudca-k8token",
			Usage:  "The Kubeconfig Token b64 encoded",
			EnvVar: "KUBERNETES_K8TOKEN",
			Value:  "",
		},
		mcnflag.StringFlag{
			Name:   "cloudca-userdata",
			Usage:  "A user-data file to be passed to cloud-init",
			EnvVar: "KUBERNETES_USERDATA",
			Value:  "",
		},
		mcnflag.StringFlag{
			Name:   "cloudca-image",
			Usage:  "kubernetes image to run",
			EnvVar: "KUBERNETES_IMAGE",
			Value:  "",
		},
	}
}

// SetConfigFromFlags initializes the driver based on the command line flags.
func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.Userdata = flags.String("cloudca-userdata")
	d.Image = flags.String("cloudca-image")
	d.kubernetesToken = flags.String("cloudca-k8token")
	d.SetSwarmConfigFromFlags(flags)

	if d.Image == "" {
		d.Image = defaultImage
	}

	return nil
}

// GetSSHHostname returns hostname for use with ssh
func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// GetSSHUsername returns username for use with ssh
func (d *Driver) GetSSHUsername() string {
	if d.SSHUser == "" {
		d.SSHUser = defaultUser
	}
	return d.SSHUser
}



// PreCreateCheck is called to enforce pre-creation steps
func (d *Driver) PreCreateCheck() error {
	if d.Userdata != "" {
		// Check we can read user data
		_, err := ioutil.ReadFile(d.Userdata)
		if err != nil {
			return fmt.Errorf("cannot read userdata file %v: %v", d.Userdata, err)
		}
	}

	return nil
}


// Create creates a pod VM instance acting as a docker host.
func (d *Driver) Create() error {
	log.Infof("Generating SSH Key")

	if err := ssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
		return err
	}

	log.Infof("Creating host...")
	return d.Start()
}


func getWaitForIP(ctx context.Context, k8s kubernetes.Interface, namespace, name string) (string, error) {
	_, err := k8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	w, err := k8s.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector:  "metadata.name=" + name,
		TimeoutSeconds: &[]int64{600}[0],
	})
	if err != nil {
		return "", err
	}

	var ip string
	for event := range w.ResultChan() {
		if pod, ok := event.Object.(*corev1.Pod); ok {
			if pod.Status.PodIP != "" {
				ip = pod.Status.PodIP
				w.Stop()
			}
		}
	}

	if ip == "" {
		return "", fmt.Errorf("failed to get IP of %s/%s", namespace, name)
	}

	return ip, nil
}

func (d *Driver) getClient() (string, kubernetes.Interface, apply.Apply, error) {
	// kubeconfigloc := "/.kube/config"
	base64decodefile(d.kubernetesToken)
	// kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigloc)
    // if err != nil {
    //     return "",nil,nil, fmt.Errorf("error loading kubernetes configuration: %w", err)
    // }
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	ns, _, err := loader.Namespace()
	if err != nil {
		return "", nil, nil, err
	}
	cfg, err := loader.ClientConfig()
	if err != nil {
		return "", nil, nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", nil, nil, err
	}
	apply, err := apply.NewForConfig(cfg)
	if err != nil {
		return "", nil, nil, err
	}
	return ns, client, apply.WithDynamicLookup(), err
}



// GetURL returns the URL of the remote docker daemon.
func (d *Driver) GetURL() (string, error) {
	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("tcp://%s", net.JoinHostPort(ip, "2376")), nil
}

// GetIP returns the IP address of the pod instance.
func (d *Driver) GetIP() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	namespace, k8s, _, err := d.getClient()
	if err != nil {
		return "", err
	}

	return getWaitForIP(ctx, k8s, namespace, d.MachineName)
}

// GetState returns a docker.hosts.state.State value representing the current state of the host.
func (d *Driver) GetState() (state.State, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	namespace, k8s, _, err := d.getClient()
	if err != nil {
		return state.None, err
	}

	pod, err := k8s.CoreV1().Pods(namespace).Get(ctx, d.MachineName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return state.None, nil
	} else if err != nil {
		return state.None, err
	}

	switch pod.Status.Phase {
	case corev1.PodPending:
		return state.Starting, nil
	case corev1.PodRunning:
		return state.Running, nil
	default:
		return state.Stopped, nil
	}
	return state.None, nil
}

// Start starts an existing pod instance or create an instance with an existing disk.
func (d *Driver) Start() error {
	if err := d.Stop(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	pubKeyData, err := ioutil.ReadFile(d.ResolveStorePath("id_rsa.pub"))
	if err != nil {
		return err
	}

	var userdata []byte
	if d.Userdata != "" {
		userdata, err = ioutil.ReadFile(d.Userdata)
		if err != nil {
			return err
		}
	}

	metadata, err := json.Marshal(map[string]interface{}{
		"public-keys": []interface{}{
			string(pubKeyData),
		},
	})
	if err != nil {
		return err
	}

	namespace, k8s, apply, err := d.getClient()
	if err != nil {
		return err
	}

	pod, secret := podAndSecret(namespace, d.MachineName, d.Image, userdata, metadata)
	apply, os := getApply(ctx, apply, pod, secret)

	if err := apply.Apply(os); err != nil {
		return err
	}

	w, err := k8s.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		TimeoutSeconds: &[]int64{600}[0],
	})
	if err != nil {
		return err
	}

	for event := range w.ResultChan() {
		if pod, ok := event.Object.(*corev1.Pod); ok {
			if pod.Status.PodIP != "" {
				d.IPAddress = pod.Status.PodIP
				w.Stop()
			}
		}
	}

	if d.IPAddress == "" {
		return fmt.Errorf("failed to get IP of %s/%s", namespace, d.MachineName)
	}

	return nil
}

func podAndSecret(namespace, name, image string, userData, metaData []byte) (*corev1.Pod, *corev1.Secret) {
	return &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						//make volume using default storage class
						Name: "cache-volume",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "k8-core",
							},
						},
					},
					{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: name,
							},
						},
					},
				},
				Containers: []corev1.Container{{
					Name:  "machine",
					Image: image,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 22,
							Name:          "ssh",
						},
						{
							ContainerPort: 6443,
							Name:          "kube-api",
						},
						{
							ContainerPort: 9435,
							Name:          "endpoint",
						},
						{
							ContainerPort: 443,
							Name:          "https",
						},
						{
							ContainerPort: 80,
							Name:          "http",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "cache-volume",
							MountPath: "/var/lib/rancher",
						},
						{
							Name:      "data",
							MountPath: "/var/lib/cloud/seed/nocloud/meta-data",
							SubPath:   "meta-data",
						},
						{
							Name:      "data",
							MountPath: "/var/lib/cloud/seed/nocloud/user-data",
							SubPath:   "user-data",
						},
					},
					// Give the pod 2GB ram and cpu limit
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &[]bool{true}[0],
					},
					Stdin:     true,
					StdinOnce: true,
					TTY:       true,
				}},
				RestartPolicy:                 corev1.RestartPolicyNever,
				AutomountServiceAccountToken:  new(bool),
				Hostname:                      name,
				TerminationGracePeriodSeconds: new(int64),
			},
		},
		&corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Data: map[string][]byte{
				"user-data": userData,
				"meta-data": metaData,
			},
		}
}

func getApply(ctx context.Context, apply apply.Apply, pod *corev1.Pod, secret *corev1.Secret) (apply.Apply, *objectset.ObjectSet) {
	os := objectset.NewObjectSet(pod, secret)
	return apply.
		WithDynamicLookup().
		WithListerNamespace(pod.Namespace).
		WithOwner(pod).
		WithGVK(os.GVKs()...).
		WithContext(ctx), os
}

// Stop stops an existing pod instance.
func (d *Driver) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	namespace, _, apply, err := d.getClient()
	if err != nil {
		return err
	}

	pod, secret := podAndSecret(namespace, d.MachineName, "", nil, nil)
	apply, _ = getApply(ctx, apply, pod, secret)

	// Delete everything
	if err := apply.ApplyObjects(); err != nil {
		return err
	}

	d.IPAddress = ""
	return nil
}

// Restart restarts a machine which is known to be running.
func (d *Driver) Restart() error {
	if err := d.Stop(); err != nil {
		return err
	}

	return d.Start()
}

// Kill stops an existing pod instance.
func (d *Driver) Kill() error {
	return d.Stop()
}

// Remove deletes the Pod
func (d *Driver) Remove() error {
	return d.Stop()
}
