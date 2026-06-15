#!/bin/bash

kubebuilder init --domain fabricops.io --repo github.com/dpereowei/fabricops
kubebuilder create api --group fabricops --version v1alpha1 --kind FabricNetwork