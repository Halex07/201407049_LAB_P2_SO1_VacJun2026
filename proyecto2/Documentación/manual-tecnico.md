# Manual Técnico — Q.M.2026.K8s
## Proyecto #2 — Sistemas Operativos 1
### Universidad San Carlos de Guatemala — Facultad de Ingeniería
### Henry Alexander García Montúfar
### Carnet: 201407049

---

## 1. Arquitectura General

El proyecto implementa una plataforma distribuida de quiniela del Mundial 2026 desplegada en Google Cloud Platform utilizando Google Kubernetes Engine (GKE).

### Diagrama de flujo del sistema

```
Locust → Gateway API (34.128.139.94) → API REST (Rust)
       → Go Client (gRPC Client) → Go Server (gRPC Server + RabbitMQ Writer)
       → RabbitMQ → Go Consumer → Valkey (KubeVirt VM1)
                                → Grafana (KubeVirt VM2) ← lee de Valkey
```
![alt text](Imagen.png)

### Componentes del sistema

| Componente | Tecnología | Rol | Namespace |
|---|---|---|---|
| Generador de carga | Locust | Simula usuarios enviando predicciones | Local |
| Entrada | Kubernetes Gateway API | Expone el sistema públicamente | qm2026 |
| API REST | Rust + Actix-web | Recibe peticiones HTTP de Locust | qm2026 |
| gRPC Client | Go (Deployment 1) | Reenvía datos via gRPC al servidor | qm2026 |
| gRPC Server + Writer | Go (Deployment 2) | Recibe gRPC y publica en RabbitMQ | qm2026 |
| Message Broker | RabbitMQ | Cola de mensajes principal | qm2026 |
| Consumer | Go | Consume mensajes y guarda en Valkey | qm2026 |
| Base de datos | Valkey | Almacenamiento en memoria de predicciones | KubeVirt VM1 |
| Visualización | Grafana | Dashboards de métricas | KubeVirt VM2 |
| Registry | Zot | Registro de imágenes Docker y OCI Artifacts | VM externa GCP |

### Infraestructura GCP

- **Proyecto GCP:** sopes1-p2-201407049
- **Clúster GKE:** qm2026-cluster (us-east4-c)
- **Tipo de nodos:** N1-standard-4 (requerido para KubeVirt)
- **Node Pool:** kvm-pool (2 nodos)
- **VM Zot:** zot-registry (e2-medium, us-east4-c)
- **IP Gateway:** 34.128.139.94
- **IP Grafana:** 34.182.186.134:3000

---

## 2. Flujo Completo de Datos

1. **Locust** genera predicciones JSON aleatorias con equipos (ESP, MEX, BRA, ARG, GTM), goles (0-5) y usuarios (user_1 a user_1000)
2. Las predicciones se envían via HTTP POST a `http://34.128.139.94/grpc-201407049`
3. **Kubernetes Gateway API** enruta la petición al servicio `rust-api:8080`, reescribiendo la URL a `/predict`
4. **API REST (Rust)** recibe el JSON y lo reenvía al servicio `go-client:8080` via HTTP
5. **Go Client** actúa como cliente gRPC e invoca `SendPrediction` en el `go-server:50051`
6. **Go Server** recibe la solicitud gRPC y publica el mensaje en RabbitMQ (cola `predictions`)
7. **Go Consumer** consume los mensajes de RabbitMQ y almacena los datos procesados en Valkey
8. **Grafana** lee los datos de Valkey via plugin Redis y los visualiza en dashboards

### Estructura del mensaje JSON

```json
{
  "home_team": "ESP",
  "away_team": "MEX",
  "home_goals": 2,
  "away_goals": 1,
  "username": "user_42",
  "timestamp": "2026-06-26T01:00:00Z"
}
```

---

## 3. Configuración de Gateway API

Se utiliza Kubernetes Gateway API en lugar de Ingress Controller, usando la GatewayClass nativa de GKE `gke-l7-global-external-managed`.

### GatewayClass y Gateway

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: qm2026-gateway
  namespace: qm2026
spec:
  gatewayClassName: gke-l7-global-external-managed
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    allowedRoutes:
      namespaces:
        from: Same
```

### HTTPRoute

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: qm2026-route
  namespace: qm2026
spec:
  parentRefs:
  - name: qm2026-gateway
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /grpc-201407049
    filters:
    - type: URLRewrite
      urlRewrite:
        path:
          type: ReplacePrefixMatch
          replacePrefixMatch: /predict
    backendRefs:
    - name: rust-api
      port: 8080
```

### Rutas configuradas

| Ruta | Destino | Descripción |
|---|---|---|
| `/grpc-201407049` | rust-api:8080 | Flujo principal via gRPC |

### IP pública del Gateway

- **IP:** 34.128.139.94
- **Estado:** PROGRAMMED = True

![alt text](Imagen1.png)

---

## 4. Comunicación REST y gRPC

### REST (Locust → Rust → Go Client)

- **Protocolo:** HTTP/1.1
- **Formato:** JSON
- **Puerto:** 8080
- **Endpoint:** POST /predict

### gRPC (Go Client → Go Server)

- **Protocolo:** HTTP/2
- **Formato:** Protocol Buffers
- **Puerto:** 50051
- **Servicio:** MatchPredictionService
- **Método:** SendPrediction

### Definición Proto

```protobuf
syntax = "proto3";
package worldcup2026;
option go_package = "./proto";

message MatchPredictionRequest {
  Teams home_team = 1;
  Teams away_team = 2;
  int32 home_goals = 3;
  int32 away_goals = 4;
  string username = 5;
  string timestamp = 6;
}

enum Teams {
  TEAMS_UNKNOWN = 0;
  GTM = 1;
  MEX = 2;
  BRA = 3;
  ARG = 4;
  ESP = 5;
}

message MatchPredictionResponse {
  string status = 1;
}

service MatchPredictionService {
  rpc SendPrediction (MatchPredictionRequest) returns (MatchPredictionResponse);
}
```

---

## 5. RabbitMQ

RabbitMQ es el broker principal de mensajería del proyecto.

### Configuración

- **Cola:** `predictions`
- **Tipo:** Durable (persiste reinicios)
- **Broker URL:** `amqp://guest:guest@rabbitmq:5672/`
- **Publisher:** Go Server (Deployment 2)
- **Consumer:** Go Consumer

### Deployment en Kubernetes

```yaml
image: rabbitmq:3-management
ports:
- containerPort: 5672  # AMQP
- containerPort: 15672 # Management UI
```

![alt text](Imagen2.png)
---

## 6. Despliegue de Valkey y Grafana en KubeVirt

### KubeVirt

KubeVirt permite ejecutar máquinas virtuales dentro de un clúster de Kubernetes. Se instaló en el clúster GKE con modo de emulación habilitado (`useEmulation: true`) ya que los nodos GKE no exponen KVM directamente.

```bash
kubectl patch kubevirt kubevirt -n kubevirt --type=merge -p '{
  "spec": {
    "configuration": {
      "developerConfiguration": {
        "useEmulation": true
      }
    }
  }
}'
```

### Valkey (KubeVirt VM1)

Valkey se ejecuta dentro de un contenedor gestionado por containerd en una VM administrada por KubeVirt.

**IP de la VM:** 10.28.1.11
**Puerto:** 6379

#### Proceso de instalación en la VM

```bash
# Instalar containerd 1.7.20
curl -fsSL https://github.com/containerd/containerd/releases/download/v1.7.20/containerd-1.7.20-linux-amd64.tar.gz \
  -o /tmp/containerd.tar.gz
sudo tar -xzf /tmp/containerd.tar.gz -C /usr/local

# Actualizar runc
sudo curl -fsSL https://github.com/opencontainers/runc/releases/download/v1.1.12/runc.amd64 \
  -o /usr/bin/runc
sudo chmod +x /usr/bin/runc

# Correr Valkey con containerd
sudo ctr image pull docker.io/valkey/valkey:latest
sudo ctr run -d --net-host docker.io/valkey/valkey:latest valkey-1 \
  valkey-server --bind 0.0.0.0 --port 6379
```

#### Verificación

```bash
sudo ctr tasks list
# TASK     PID    STATUS
# valkey-1 11051  RUNNING

sudo ctr tasks exec --exec-id ping1 valkey-1 redis-cli ping
# PONG
```

![alt text](Imagen3.png)

### Grafana (KubeVirt VM2)

Grafana se ejecuta en una VM independiente gestionada por KubeVirt.

**IP de la VM:** 10.28.1.12
**Puerto:** 3000
**IP pública:** 34.182.186.134:3000

#### Proceso de instalación en la VM

```bash
# Instalar Grafana directamente en la VM
sudo dnf install -y https://dl.grafana.com/oss/release/grafana-11.0.0-1.x86_64.rpm
sudo systemctl enable grafana-server
sudo systemctl start grafana-server

# Instalar plugin Redis para conectar a Valkey
sudo grafana-cli plugins install redis-datasource
sudo systemctl restart grafana-server
```

#### Configuración del Datasource

Grafana se conecta a Valkey a través del NodePort del nodo GKE:
- **Address:** 10.150.0.9:30379
- **Type:** Redis (Standalone)

![alt text](Imagen4.png)

---

## 7. Configuración de HPA

El HPA escala automáticamente el deployment de Rust cuando la CPU supera el 30%.

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: rust-api-hpa
  namespace: qm2026
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: rust-api
  minReplicas: 1
  maxReplicas: 3
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 30
```

### Resultado observado durante pruebas de carga

| CPU | Réplicas |
|---|---|
| 0% | 1 |
| 127% | 3 (escala automáticamente) |
| 196% | 3 |
| 72% | 3 |

![alt text](Imagen5.png)

---

## 8. Publicación y Consumo de Imágenes desde Zot

Zot se desplegó en una VM de GCP externa al clúster:
- **VM:** zot-registry (e2-medium)
- **IP:** 34.48.90.23
- **Puerto:** 5000 (HTTPS)
- **Versión:** 2.0.0

### Imágenes publicadas

| Imagen | Tag | Digest |
|---|---|---|
| rust-api | latest | sha256:ff98ef49... |
| go-client | latest | sha256:e1f74036... |
| go-server | latest | sha256:2d379aba... |
| go-consumer | latest | sha256:eb94d7f6... |

### Comandos de publicación

```bash
docker build -t 34.48.90.23:5000/rust-api:latest .
docker push 34.48.90.23:5000/rust-api:latest

docker build -t 34.48.90.23:5000/go-client:latest .
docker push 34.48.90.23:5000/go-client:latest
```

### OCI Artifact

El archivo `locustfile.py` fue subido como OCI Artifact a Zot:

```bash
oras push 34.48.90.23:5000/locustfile:latest \
  --insecure \
  --disable-path-validation \
  locustfile.py:text/plain
```

**Digest:** sha256:0942014bc343b7ab9027c61443e00ff41c9048aad27a92af1d25d3e59bc4ba70

El locustfile.py es descargado por Locust al iniciar las pruebas de carga, demostrando el uso de OCI Artifacts para distribución de configuración.

---

## 9. Pruebas Realizadas

### 9.1 Prueba de carga con 50 usuarios (1 réplica Go Server)

```
Comando: locust -f locustfile.py --host=http://34.128.139.94 --users=50 --spawn-rate=5 --run-time=2m
```

| Métrica | Valor |
|---|---|
| Total requests | 639 |
| Failures | 47 (7.36%) |
| Req/s | 5.33 |
| Avg response time | 7,898ms |
| Min response time | 240ms |
| Max response time | 10,644ms |

### 9.2 Prueba de carga con 100 usuarios (3 réplicas — HPA activado)

```
Comando: locust -f locustfile.py --host=http://34.128.139.94 --users=100 --spawn-rate=10 --run-time=3m
```

| Métrica | Valor |
|---|---|
| Total requests | 1,565 |
| Failures | 23 (1.47%) |
| Req/s | 8.76 |
| Avg response time | 9,801ms |
| Min response time | 179ms |
| Max response time | 28,702ms |

![alt text](Imagen6.png)

### 9.3 Análisis comparativo de réplicas

| Métrica | 1 Réplica | 3 Réplicas (HPA) |
|---|---|---|
| Usuarios | 50 | 100 |
| Req/s | 5.33 | 8.76 |
| Failures | 7.36% | 1.47% |
| Avg response | 7,898ms | 9,801ms |

**Conclusión:** Con 3 réplicas el sistema maneja el doble de usuarios con solo 1.47% de fallos vs 7.36% con 1 réplica, demostrando la efectividad del escalado horizontal.

### 9.4 Verificación del flujo completo

```bash
curl -v http://34.128.139.94/grpc-201407049 \
  -H "Content-Type: application/json" \
  -d '{"home_team":"ESP","away_team":"MEX","home_goals":2,"away_goals":1,
       "username":"user_42","timestamp":"2026-06-26T01:00:00Z"}'

# Respuesta: HTTP/1.1 200 OK
# {"status":"ok","message":"Prediction received"}
```

![alt text](Imagen7.png)

---

## 10. Dashboard de Grafana

### Equipo asignado

- **Último dígito del carnet:** 9
- **Equipo asignado:** ESP (España)

### Visualizaciones implementadas

| Panel | Tipo | Query Valkey |
|---|---|---|
| Max Goles Predichos (Local) | Stat | `GET goals:home:5` |
| Min Goles Predichos (Local) | Stat | `GET goals:home:0` |
| Max Goles Predichos (Visitante) | Stat | `GET goals:away:5` |
| Min Goles Predichos (Visitante) | Stat | `GET goals:away:0` |
| Moda Goles Local | Stat | `GET goals:home:2` |
| Moda Goles Visitante | Stat | `GET goals:away:2` |
| Top Equipos con Más Victorias | Bar Gauge | `GET team:X:wins` |
| País/Equipo Asignado (ESP) | Stat | `GET team:ESP:total_predictions` |
| Total Predicciones | Stat | `GET total_predictions` |
| Serie Temporal ESP | Time Series | `LRANGE team:ESP:timeseries 0 100` |

![alt text](Imagen8.png)

### Datos almacenados en Valkey

Las keys principales en Valkey son:

```
total_predictions          → contador total de predicciones
team:ESP:total_predictions → predicciones del equipo asignado
team:ESP:wins              → victorias de ESP
team:ESP:home_goals        → lista de goles como local
team:ESP:away_goals        → lista de goles como visitante
team:ESP:timeseries        → serie temporal con timestamps
goals:home:N               → conteo de partidos con N goles locales (0-5)
goals:away:N               → conteo de partidos con N goles visitantes (0-5)
user:user_N:predictions    → predicciones por usuario
```

---

## 11. Conclusiones

1. **KubeVirt** permite ejecutar VMs dentro de Kubernetes manteniendo el paradigma de orquestación de contenedores. La virtualización anidada es crítica para su funcionamiento en entornos cloud.

2. **gRPC** ofrece mejor rendimiento que REST para comunicación interna entre microservicios gracias al uso de Protocol Buffers y HTTP/2, reduciendo la latencia y el tamaño de los mensajes.

3. **RabbitMQ** desacopla efectivamente los productores (Go Server) de los consumidores (Go Consumer), permitiendo que el sistema absorba picos de carga sin perder mensajes.

4. **El HPA** demostró ser efectivo escalando de 1 a 3 réplicas cuando la CPU superó el 30%, reduciendo la tasa de fallos de 7.36% a 1.47% con el doble de usuarios.

5. **Zot** como registry OCI es una alternativa ligera a Docker Hub o GCR, soportando tanto imágenes de contenedores como OCI Artifacts para distribución de configuración.

6. **Valkey** como base de datos en memoria es ideal para almacenar métricas en tiempo real por su baja latencia y estructuras de datos especializadas (listas, contadores).

7. **Gateway API** de Kubernetes ofrece más flexibilidad que Ingress Controller, permitiendo reglas de enrutamiento avanzadas como URL rewriting y filtros por path.

---

## 12. Repositorio

- **GitHub:** https://github.com/Halex07/201407049_LAB_P2_SO1_VacJun2026
- **Auxiliar:** @CamiloSincal 



