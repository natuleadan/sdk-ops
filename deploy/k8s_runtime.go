package deploy

import (
	"fmt"
	"path/filepath"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

const k8sRuntimeTmpl = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  labels:
    app: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: %s
        image: %s
        ports:
        - containerPort: %d
---
apiVersion: v1
kind: Service
metadata:
  name: %s
spec:
  selector:
    app: %s
  ports:
  - port: 80
    targetPort: %d
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s
  annotations:
    kubernetes.io/ingress.class: traefik
spec:
  rules:
  - host: %s
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: %s
            port:
              number: 80
`

func DeployK3s(client *goss.Client, name, imageRef, domain string, port int) error {
	if port == 0 {
		port = 8080
	}

	yaml := fmt.Sprintf(k8sRuntimeTmpl,
		name, name, name, name, name, imageRef, port,
		name, name, port,
		name, domain, name,
	)

	tmpFile := fmt.Sprintf("/tmp/sdk-k8s-%s.yaml", name)
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	go func() {
		defer stdin.Close()
		stdin.Write([]byte(yaml))
	}()

	out, err := sess.CombinedOutput(fmt.Sprintf("cat > %s && kubectl apply -f %s && rm -f %s", tmpFile, tmpFile, tmpFile))
	if err != nil {
		return fmt.Errorf("k3s deploy: %w\n%s", err, string(out))
	}
	fmt.Printf("  → Applied k3s manifests for %s\n", name)
	return nil
}

func DeployK3sFromCompose(client *goss.Client, name, versionDir, imageRef string) error {
	domain := ""
	port := 8080

	svcYaml := filepath.Join(versionDir, "service.yaml")
	out, _, err := ssh.Run(client, fmt.Sprintf("cat %s 2>/dev/null || true", svcYaml))
	if err == nil {
		for line := range strings.SplitSeq(out, "\n") {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "domain:"); ok {
				domain = strings.TrimSpace(after)
			}
			if strings.HasPrefix(line, "port:") {
				fmt.Sscanf(line, "port: %d", &port)
			}
			if strings.HasPrefix(line, "ports:") {
				// Parse "80:80" format
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					fmt.Sscanf(parts[len(parts)-1], "%d", &port)
				}
			}
			if after, ok := strings.CutPrefix(line, "image:"); ok {
				if imageRef == "" {
					imageRef = strings.TrimSpace(after)
				}
			}
		}
	}

	if imageRef == "" {
		imageRef = fmt.Sprintf("%s:latest", name)
	}

	return DeployK3s(client, name, imageRef, domain, port)
}
