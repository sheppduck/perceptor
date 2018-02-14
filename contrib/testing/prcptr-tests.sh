# Copyright (C) 2018 Synopsys, Inc.
#!/bin/sh

export PERCEPTOR_POD_NAMESPACE="perceptorTestPod"

# TODO Put in a check here if kubectl cli is present

# Spin up a Kube POD using busybox
echo "Creating POD..."
kubectl run busybox --image=busybox --env="POD_NAMESPACE=$PERCEPTOR_POD_NAMESPACE"

# TODO Verify perceptor is notified of new POD/Image - not sure how yet...

# Check POD has been annotated with Black Duck
tstAnnotate() {
  WAIT_TIME=$((30))
  echo "Checking for presense Blackduck POD annotations..."
  sleep $WAIT_TIME
  a_state = $(kubectl describe pod $PERCEPTOR_POD_NAMESPACE | grep "blackduck")
  if [[ -z $a_state ]]; then
    echo "There appears to be no POD Annoations present."
    exit 1;
  else
    echo "Annoations found!"
  fi
}