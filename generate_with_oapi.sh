#! /usr/env/bash

oapi-codegen -config oapi-models.yaml openapi.yaml
oapi-codegen -config oapi-server.yaml openapi.yaml