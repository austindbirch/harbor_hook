FROM envoyproxy/envoy:v1.31-latest

# Install wget for health checks
RUN apt-get update && apt-get install -y wget && rm -rf /var/lib/apt/lists/*

COPY envoy.yaml /etc/envoy/envoy.yaml
COPY certs/ /etc/envoy/certs/

# Make sure certificates have correct permissions for envoy user
RUN chmod 644 /etc/envoy/certs/*.key
RUN chmod 644 /etc/envoy/certs/*.crt
RUN chown -R envoy:envoy /etc/envoy/certs/

CMD ["/usr/local/bin/envoy", "-c", "/etc/envoy/envoy.yaml"]
