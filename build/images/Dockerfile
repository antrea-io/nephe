#builder
FROM golang:1.21 as builder

# install controller-gen
RUN	CONTROLLER_GEN_TMP_DIR=$(mktemp -d) &&\
	cd $CONTROLLER_GEN_TMP_DIR && \
	go mod init tmp  && \
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0 &&\
	rm -rf $$CONTROLLER_GEN_TMP_DIR

# install kustomize
RUN	KUSTOMIZE_TMP_DIR=$(mktemp -d) &&\
	cd $KUSTOMIZE_TMP_DIR && \
	go mod init tmp  && \
	go install sigs.k8s.io/kustomize/kustomize/v4@v4.5.2 && \
	rm -rf $$KUSTOMIZE_TMP_DIR

# install mockgen
RUN	GOMOCK_TMP_DIR=$(mktemp -d) &&\
	cd $GOMOCK_TMP_DIR && \
	go mod init tmp  && \
	go install github.com/golang/mock/mockgen@v1.6.0 &&\
	rm -rf $$GOMOCK_GEN_DIR

# install golangci-lint
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.54.0

ENV PATH=$PATH:$GOPATH/bin

# product
# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot as product
WORKDIR /
COPY ./bin/*controller /
USER nonroot:nonroot
