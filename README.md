# Exchange Rates API operator

An operator using the exchange rates api. This operator uses the following image of the repo [https://github.com/ervitis/exchangerateapp](https://github.com/ervitis/exchangerateapp)

## DONE

- Reconcile controller for the operator
- Run operator inside localhost cluster (minikube)
- Logs to see state
- Modify status table when describe pod status
- Define ENV for exchange rates api: configmap or secret
- Create service

## DOING

- Attach db (optional) to the app and use CRUD operations

## TODO

- Update to daemonSet
- Create ingress
- Create volume for the secrets
