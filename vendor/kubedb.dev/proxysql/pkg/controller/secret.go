/*
Copyright The KubeDB Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package controller

import (
	"context"
	"fmt"

	api "kubedb.dev/apimachinery/apis/kubedb/v1alpha1"
	"kubedb.dev/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha1/util"

	"github.com/appscode/go/crypto/rand"
	core "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	core_util "kmodules.xyz/client-go/core/v1"
)

const (
	proxysqlUser = "proxysql"
)

func (c *Controller) ensureProxySQLSecret(proxysql *api.ProxySQL) error {
	if proxysql.Spec.ProxySQLSecret == nil {
		secretVolumeSource, err := c.createProxySQLSecret(proxysql)
		if err != nil {
			return err
		}

		proxysqlPathced, _, err := util.PatchProxySQL(
			context.TODO(),
			c.ExtClient.KubedbV1alpha1(),
			proxysql,
			func(in *api.ProxySQL) *api.ProxySQL {
				in.Spec.ProxySQLSecret = secretVolumeSource
				return in
			},
			metav1.PatchOptions{},
		)
		if err != nil {
			return err
		}
		proxysql.Spec.ProxySQLSecret = proxysqlPathced.Spec.ProxySQLSecret
	}

	return nil
}

func (c *Controller) createProxySQLSecret(proxysql *api.ProxySQL) (*core.SecretVolumeSource, error) {
	authSecretName := proxysql.Name + "-auth"

	sc, err := c.checkSecret(authSecretName, proxysql)
	if err != nil {
		return nil, err
	}

	owner := metav1.NewControllerRef(proxysql, api.SchemeGroupVersion.WithKind(api.ResourceKindProxySQL))

	if sc == nil {
		randProxysqlPassword := ""

		// if the password starts with "-", it will cause error in bash scripts (in proxysql-tools)
		for randProxysqlPassword = rand.GeneratePassword(); randProxysqlPassword[0] == '-'; {
		}

		secret := &core.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      authSecretName,
				Namespace: proxysql.Namespace,
				Labels:    proxysql.OffshootSelectors(),
			},
			Type: core.SecretTypeOpaque,
			StringData: map[string]string{
				api.ProxySQLUserKey:     proxysqlUser,
				api.ProxySQLPasswordKey: randProxysqlPassword,
			},
		}

		core_util.EnsureOwnerReference(&secret.ObjectMeta, owner)

		if _, err := c.Client.CoreV1().Secrets(proxysql.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			return nil, err
		}
	}
	return &core.SecretVolumeSource{
		SecretName: authSecretName,
	}, nil
}

func (c *Controller) checkSecret(secretName string, proxysql *api.ProxySQL) (*core.Secret, error) {
	secret, err := c.Client.CoreV1().Secrets(proxysql.Namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		if kerr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	if secret.Labels[api.LabelDatabaseKind] != api.ResourceKindProxySQL ||
		secret.Labels[api.LabelProxySQLName] != proxysql.Name {
		return nil, fmt.Errorf(`intended secret "%v/%v" already exists`, proxysql.Namespace, secretName)
	}
	return secret, nil
}
