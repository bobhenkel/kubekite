#!/bin/bash

[ -z ${BUILDKITE_BRANCH} ] && echo "BUILDKITE_BRANCH env is required" && exit 1

# This is an associative array of git branches for which we deploy to Kube.
# It designates certain branches for deployment to certain Kube contexts.
# The syntax is [branchname]=context.
#
# If a branch is not in this array, this deploy script will not deploy it.
#
declare -A deployable_branches
deployable_branches=( [master]=your-kube-cluster.yourdomain.com ) 

branch_is_deployable() {
   [[ -n ${deployable_branches[$1]} || -z ${deployable_branches[$1]-foo} ]]
}

if ! branch_is_deployable $BUILDKITE_BRANCH; then
   echo "Branch ${BUILDKITE_BRANCH} has not been designated as Kube-deployable."
   echo "Skipping deployment."
   exit 0
fi

DEPLOY_CONTEXT=${deployable_branches[$BUILDKITE_BRANCH]}

# Set a cluster context.  This is referenced in our kubeconfig and must be present in that file.
KUBE_CONTEXT="${DEPLOY_CONTEXT}-buildkite-deploy"

# The base kubectl command.  This can include any applicable context, namespace, etc
KUBECTL="kubectl --kubeconfig kube-deploy/${DEPLOY_CONTEXT}/kubeconfig \
                 --context ${KUBE_CONTEXT}"

# Deployment timeout, in seconds (max time to wait for deployment to complete)
# Default: 60 seconds
DEPLOYMENT_TIMEOUT=${DEPLOYMENT_TIMEOUT:-300}

exists () {
  type "$1" >/dev/null 2>/dev/null
} 

gen_deployment_yaml() {
  cat kube-deploy/${DEPLOY_CONTEXT}/deployment.yaml | sed -e "s/%VERSION%/${BUILDKITE_COMMIT}/g"
}

poll_deployment_status() {
  local unavailable_count=1
  local re='^[0-9]+$'
  start_time=$(date +%s)
  echo "Polling deployment; waiting for pool of unavailable pods to reach zero..."
  while [[ "$unavailable_count" =~ ^[0-9]+$ && "$unavailable_count" -gt "0" ]]; do
    sleep 5
    unavailable_count=$(gen_deployment_yaml | \
        $KUBECTL get -f - -o 'jsonpath={range .items[*]}{.status.unavailableReplicas}')
    if ! [[ $unavailable_count =~ $re ]]; then
      echo "Pool of unavailable pods has reached zero."
      echo "Active deployment appears to be in a functional state."
    else
      now=$(date +%s)
      difference=$(($now-$start_time))
      echo "Pods unavailable: $unavailable_count  Time Elapsed: $difference  [Timeout: ${DEPLOYMENT_TIMEOUT}]"
      if [ "$difference" -gt "$DEPLOYMENT_TIMEOUT" ]; then
        echo "Deployment has timed out after $DEPLOYMENT_TIMEOUT seconds"
        return 1
      fi
    fi
  done
  echo "Deployment is 100% online.  Great success!"
  gen_deployment_yaml | $KUBECTL get -f -
  return 0
}

poll_deployment_rollback_status() {
  local unavailable_count=1
  local re='^[0-9]+$'
  start_time=$(date +%s)
  echo "Polling deployment roll-back; waiting for pool of unavailable pods to reach zero..."
  while [[ "$unavailable_count" =~ ^[0-9]+$ && "$unavailable_count" -gt "0" ]]; do
    sleep 5
    unavailable_count=$(gen_deployment_yaml | \
        $KUBECTL get -f - -o 'jsonpath={range .items[*]}{.status.unavailableReplicas}')
    if ! [[ $unavailable_count =~ $re ]]; then
      echo "Pool of unavailable pods has reached zero."
    else
      now=$(date +%s)
      difference=$(($now-$start_time))
      echo "Pods unavailable: $unavailable_count  Time Elapsed: $difference  [Timeout: ${DEPLOYMENT_TIMEOUT}]"
      if [ "$difference" -gt "$DEPLOYMENT_TIMEOUT" ]; then
        echo "Roll-back has timed out after $DEPLOYMENT_TIMEOUT seconds"
        return 1
      fi
    fi
  done
  echo "Deployment is 100% online.  Roll-back appears to have been successful."
  gen_deployment_yaml | $KUBECTL get -f -
  return 0
}


rollback() {
  gen_deployment_yaml | \
     $KUBECTL rollout undo -f -
  if poll_deployment_rollback_status; then
    return 0
  else
    echo "Roll-back failed. You will have to roll back manually."
    return 1
  fi
}


if ! exists kubectl; then
   echo "Error: kubectl is not in our PATH.  Please install kubectl."
   exit 1
fi

if [ ! -r kube-deploy/${DEPLOY_CONTEXT}/kubeconfig ]; then
   echo "Kube config not found in `pwd`/kube-deploy/${DEPLOY_CONTEXT}/kubeconfig"
   echo "Please add one with proper credentials and try again."
   exit 1
fi

if [ -r kube-deploy/${DEPLOY_CONTEXT}/deployment.yaml ]; then
   if ! grep -q "%VERSION%" kube-deploy/${DEPLOY_CONTEXT}/deployment.yaml; then
      echo "Your deployment.yaml must contain a %VERSION% placeholder in the image name."
      exit 1
   fi

   echo "Applying deployment.yaml ..."
   gen_deployment_yaml | \
        ${KUBECTL} apply -f -

   if [ $? -ne 0 ]; then
      echo "Deployment failed to apply.  Oh no!"
      exit 1
   fi

   if poll_deployment_status -eq 0; then
      echo "Deployment complete"
   else 
      echo "Deployment failed.  Attempting to roll-back."
      if rollback -gt 0; then
         # Rollback failed
         exit 2
      fi
      exit 1
   fi
fi

exit 0
