# Override Lease

`OverrideLease` は、Kubernetes リソースの一部フィールドを期限付きで変更し、期限切れまたは `OverrideLease` の削除時に元の値へ戻す Controller です。

適用前の値を保存し、ロールバック時には Controller が設定した値のままかを確認します。ほかの actor が変更した値を強制的に上書きすることはありません。対象リソースは `OverrideLease` と同じ namespace に限られます。

## インストール

Controller イメージを指定して、CRD、RBAC、Controller をデプロイします。

```console
make deploy IMG=ghcr.io/mitsu3s/override-lease:v0.0.1
```

CRD だけをインストールする場合は `make install` を使用します。

## OverrideLease のサンプル

次の例は、`checkout` namespace にある `HorizontalPodAutoscaler/checkout` の `maxReplicas` を7日間だけ `50` に変更します。

```yaml
apiVersion: mitsu3s.dev/v1alpha1
kind: OverrideLease
metadata:
  name: checkout-incident-headroom
  namespace: checkout
spec:
  targetRef:
    apiVersion: autoscaling/v2
    kind: HorizontalPodAutoscaler
    name: checkout
  overrides:
    - path: /spec/maxReplicas
      value: 50
  duration: 7d
  reason: "INC-XXX: temporary checkout capacity"
  ticket: "INC-XXX"
  driftPolicy: FailOnDrift
```

`spec` の各フィールドは次の意味を持ちます。

| フィールド    | 必須   | 内容                                                   |
| ------------- | ------ | ------------------------------------------------------ |
| `targetRef`   | はい   | 変更するリソースの `apiVersion`、`kind`、`name`        |
| `overrides`   | はい   | 変更するフィールドの JSON Pointer と一時的な値。1〜8件 |
| `duration`    | はい   | 作成時刻からの有効期間。`30m`、`2h`、`7d`、`1w2d` など |
| `reason`      | はい   | 変更理由。8〜512文字                                   |
| `ticket`      | いいえ | インシデント、変更申請、承認などの参照                 |
| `driftPolicy` | いいえ | 外部変更を検出したときの動作。既定値は `FailOnDrift`   |

`duration` では `ms`、`s`、`m`、`h`、`d`、`w` を組み合わせられます。`1d` は24時間、`1w` は7日です。期限は `metadata.creationTimestamp + spec.duration` で計算され、絶対時刻は Controller が `status.expiresAt` に記録します。既定の上限は `30d` で、Controller の `--max-lease-duration` から変更できます。

`FailOnDrift` は、適用中の値がほかの actor に変更されたとき、安全に戻せるフィールドをロールバックして `Conflict` になります。`Ignore` は期限まで待ちますが、どちらの場合も外部で変更された値をロールバックで上書きしません。

## 対応リソース

| リソース                                 | `path`                                       | `value`                                |
| ---------------------------------------- | -------------------------------------------- | -------------------------------------- |
| `apps/v1 Deployment`                     | `/spec/replicas`                             | 0以上の整数                            |
| `apps/v1 StatefulSet`                    | `/spec/replicas`                             | 0以上の整数                            |
| `autoscaling/v2 HorizontalPodAutoscaler` | `/spec/minReplicas`, `/spec/maxReplicas`     | 0以上の整数                            |
| `batch/v1 CronJob`                       | `/spec/suspend`                              | `true` または `false`                  |
| `policy/v1 PodDisruptionBudget`          | `/spec/minAvailable`, `/spec/maxUnavailable` | 0以上の整数または `"25%"` のような割合 |

`targetRef` と `overrides` は作成後に変更できません。`duration` は有効な間に短縮または延長できます。

## 適用と確認

サンプルの namespace、対象リソース名、変更内容を環境に合わせて編集してから適用します。

```console
kubectl apply -k config/samples
kubectl get overrideleases.mitsu3s.dev -n checkout
kubectl describe overridelease checkout-incident-headroom -n checkout
```

通常は `Prepared`、`Applied`、`Completed` の順に進みます。対象がまだ存在しない場合やフィールドがほかの `OverrideLease` に使用されている場合は `Pending` で待機します。安全に復元できない外部変更を検出した場合は `Conflict`、入力が対応範囲外の場合は `Invalid` になります。

期限前に削除した場合も即時ロールバックを試みます。

```console
kubectl delete overridelease checkout-incident-headroom -n checkout
```
