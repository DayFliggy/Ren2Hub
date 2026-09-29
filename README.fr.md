<div align="center">

![new-api](/frontend/public/logo.png)

# New API

🍥 **Passerelle de modèles étendus de nouvelle génération et système de gestion d'actifs d'IA**

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <a href="./README.md">English</a> |
  <strong>Français</strong> |
  <a href="./README.ja.md">日本語</a>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/Calcium-Ion/new-api/main/LICENSE">
    <img src="https://img.shields.io/github/license/Calcium-Ion/new-api?color=brightgreen" alt="licence">
  </a><!--
  --><a href="https://github.com/Calcium-Ion/new-api/releases/latest">
    <img src="https://img.shields.io/github/v/release/Calcium-Ion/new-api?color=brightgreen&include_prereleases" alt="version">
  </a><!--
  --><a href="https://hub.docker.com/r/CalciumIon/new-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a>
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/badge.svg"/>
  </a>
</p>

<p align="center">
  <a href="https://trendshift.io/repositories/20180" target="_blank">
    <img src="https://trendshift.io/api/badge/repositories/20180" alt="QuantumNous%2Fnew-api | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/>
  </a>
  <br>
  <a href="https://hellogithub.com/repository/QuantumNous/new-api" target="_blank">
    <img src="https://api.hellogithub.com/v1/widgets/recommend.svg?rid=539ac4217e69431684ad4a0bab768811&claim_uid=tbFPfKIDHpc4TzR" alt="Featured｜HelloGitHub" style="width: 250px; height: 54px;" width="250" height="54" />
  </a><!--
  -->
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/new_badge.svg" width="250" height="55" />
  </a>
</p>

<p align="center">
  <a href="#-démarrage-rapide">Démarrage rapide</a> •
  <a href="#-fonctionnalités-clés">Fonctionnalités clés</a> •
  <a href="#-déploiement">Déploiement</a> •
  <a href="#-documentation">Documentation</a> •
  <a href="#-aide-et-support">Aide</a>
</p>

</div>

## 📝 Description du projet

Cette copie du projet est Ren2Hub. Consultez la [documentation locale](docs/README.md) pour les contrats actuels de routage, d'admission, d'API et de publication. La présentation et les attributions du projet amont sont conservées. Utilisez le [guide d'installation Ren2Hub](docs/installation/DOCKER.md) ; les images amont ne contiennent pas les modifications de cette copie. Ren2Hub utilise une admission FIFO locale dans un seul processus et impose un remplacement séquentiel des versions.

La facturation utilise le solde du portefeuille, avec un quota Token supplémentaire. Les abonnements et la gestion des forfaits du site ont été supprimés ; les données comptables historiques restent protégées. L'[architecture de facturation](docs/billing/IMPLEMENTATION.md) décrit le contrat de facturation actuel.

> [!IMPORTANT]
> - Ce projet est exclusivement destiné aux scénarios de passerelle API d'IA légalement autorisés, d'authentification organisationnelle, de gestion multi-modèles, d'analyse d'utilisation, de comptabilisation des coûts et de déploiement privé.
> - Les utilisateurs doivent obtenir légalement les clés API, comptes, services de modèles et autorisations d'interface en amont, et doivent respecter les conditions d'utilisation en amont et les lois et réglementations applicables.
> - Les utilisateurs doivent s'assurer que leur utilisation est conforme aux conditions d'utilisation en amont et aux lois et réglementations applicables.
> - Lors de la fourniture de services d'IA générative au public, les utilisateurs doivent se conformer aux exigences réglementaires applicables et remplir toutes les obligations d'enregistrement, de licence, de sécurité du contenu, de vérification d'identité, de conservation des journaux, de fiscalité et d'autorisation en amont requises par leur juridiction.

---

## 🤝 Partenaires de confiance

<p align="center">
  <em>Sans ordre particulier</em>
</p>

<p align="center">
  <a href="https://www.cherry-ai.com/" target="_blank">
    <img src="./docs/images/cherry-studio.png" alt="Cherry Studio" height="80" />
  </a><!--
  --><a href="https://github.com/iOfficeAI/AionUi/" target="_blank">
    <img src="./docs/images/aionui.png" alt="Aion UI" height="80" />
  </a><!--
  --><a href="https://bda.pku.edu.cn/" target="_blank">
    <img src="./docs/images/pku.png" alt="Université de Pékin" height="80" />
  </a><!--
  --><a href="https://www.compshare.cn/?ytag=GPU_yy_gh_newapi" target="_blank">
    <img src="./docs/images/ucloud.png" alt="UCloud" height="80" />
  </a><!--
  --><a href="https://www.aliyun.com/" target="_blank">
    <img src="./docs/images/aliyun.png" alt="Alibaba Cloud" height="80" />
  </a><!--
  --><a href="https://io.net/" target="_blank">
    <img src="./docs/images/io-net.png" alt="IO.NET" height="80" />
  </a>
</p>

---

## 🙏 Remerciements spéciaux

<p align="center">
  <a href="https://www.jetbrains.com/?from=new-api" target="_blank">
    <img src="https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.png" alt="JetBrains Logo" width="120" />
  </a>
</p>

<p align="center">
  <strong>Merci à <a href="https://www.jetbrains.com/?from=new-api">JetBrains</a> pour avoir fourni une licence de développement open-source gratuite pour ce projet</strong>
</p>

---

## 🚀 Démarrage rapide

Construisez cette copie de Ren2Hub avec le [guide Docker à instance unique](docs/installation/DOCKER.md). Il construit le backend Go et le frontend Vue depuis les sources, avec PostgreSQL et Redis. Configurez les secrets requis avant le démarrage. L'application écoute sur `127.0.0.1:3000` ; les conteneurs n'ont pas de quotas CPU ou mémoire.

---

## 📚 Documentation

<div align="center">

### 📖 [Documentation officielle](https://docs.newapi.pro/en/docs) | [![Demander à DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/QuantumNous/new-api)

</div>

**Navigation rapide:**

| Catégorie | Lien |
|------|------|
| 🚀 Guide de déploiement | [Documentation d'installation](https://docs.newapi.pro/en/docs/installation) |
| ⚙️ Configuration de l'environnement | [Variables d'environnement](https://docs.newapi.pro/en/docs/installation/config-maintenance/environment-variables) |
| 📡 Documentation de l'API | [Documentation de l'API](https://docs.newapi.pro/en/docs/api) |
| ❓ FAQ | [FAQ](https://docs.newapi.pro/en/docs/support/faq) |
| 💬 Interaction avec la communauté | [Canaux de communication](https://docs.newapi.pro/en/docs/support/community-interaction) |

---

## ✨ Fonctionnalités clés

> Pour les fonctionnalités détaillées, veuillez vous référer à [Présentation des fonctionnalités](https://docs.newapi.pro/en/docs/guide/wiki/basic-concepts/features-introduction) |

### 🎨 Fonctions principales

| Fonctionnalité | Description |
|------|------|
| 🎨 Nouvelle interface utilisateur | Application Vue 3 et TypeScript construite avec Vite ; voir le [guide du frontend](frontend/README.md) |
| 🌍 Multilingue | Interface Vue : chinois simplifié et anglais |
| 🔄 Compatibilité des données | SQLite, MySQL et PostgreSQL ; les mises à jour suivent les contrats locaux de migration et de publication |
| 📈 Tableau de bord des données | Console visuelle et analyse statistique |
| 🔒 Gestion des permissions | Regroupement de jetons, restrictions de modèles, gestion des utilisateurs |

### 💰 Comptabilisation et facturation des usages autorisés

- ✅ Rechargement interne et allocation de quotas pour les scénarios légalement autorisés (EPay, Stripe)
- ✅ Comptabilisation des coûts par requête, par utilisation et par hit de cache au niveau organisationnel
- ✅ Statistiques de facturation du cache pour OpenAI, Azure, DeepSeek, Claude, Qwen et les modèles pris en charge
- ✅ Politiques de facturation flexibles pour la gestion interne ou les clients entreprise autorisés

### 🔐 Autorisation et sécurité

- Connexion par mot de passe et 2FA facultative dans l'interface Vue
- Jetons Bearer de courte durée, cookies de renouvellement HttpOnly et sessions d'appareil révocables
- Liaison de comptes OAuth et paramètres de sécurité ; le backend déclare les modules disponibles
- Les API de connexion Passkey et tierces restent contrôlées par leurs paramètres respectifs. La page de connexion Vue ne propose pas ces parcours ; voir le [contrat d'authentification](docs/authentication.md)
- 🔍 Requête de quota d'utilisation de clé (avec [new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool))

### 🚀 Fonctionnalités avancées

**Prise en charge des formats d'API:**
- ⚡ [OpenAI Responses](https://docs.newapi.pro/en/docs/api/ai-model/chat/openai/create-response)
- ⚡ [OpenAI Realtime API](https://docs.newapi.pro/en/docs/api/ai-model/realtime/create-realtime-session) (y compris Azure)
- ⚡ [Claude Messages](https://docs.newapi.pro/en/docs/api/ai-model/chat/create-message)
- ⚡ [Google Gemini](https://doc.newapi.pro/en/api/google-gemini-chat)
- 🔄 [Modèles Rerank](https://docs.newapi.pro/en/docs/api/ai-model/rerank/create-rerank) (Cohere, Jina)

**Routage intelligent:**
- ⚖️ Sélection aléatoire pondérée des canaux
- 🔄 Nouvelle tentative automatique en cas d'échec
- 🚦 Limitation du débit du modèle pour les utilisateurs

**Conversion de format:**

Chat, Responses, Claude et Gemini utilisent le même planificateur de conversion. La [matrice de prise en charge](docs/routing/GATEWAY_SUPPORT_MATRIX.md) décrit les outils, médias, citations, fins de réponse et cas explicitement refusés.

**Prise en charge de l'effort de raisonnement:**

<details>
<summary>Voir la configuration détaillée</summary>

**Modèles de la série OpenAI :**
- `o3-mini-high` - Effort de raisonnement élevé
- `o3-mini-medium` - Effort de raisonnement moyen
- `o3-mini-low` - Effort de raisonnement faible
- `gpt-5-high` - Effort de raisonnement élevé
- `gpt-5-medium` - Effort de raisonnement moyen
- `gpt-5-low` - Effort de raisonnement faible

**Modèles de pensée de Claude:**
- `claude-3-7-sonnet-20250219-thinking` - Activer le mode de pensée

**Modèles de la série Google Gemini:**
- `gemini-2.5-flash-thinking` - Activer le mode de pensée
- `gemini-2.5-flash-nothinking` - Désactiver le mode de pensée
- `gemini-2.5-pro-thinking` - Activer le mode de pensée
- `gemini-2.5-pro-thinking-128` - Activer le mode de pensée avec budget de pensée de 128 tokens
- Vous pouvez également ajouter les suffixes `-low`, `-medium` ou `-high` aux modèles Gemini pour fixer le niveau d’effort de raisonnement (sans suffixe de budget supplémentaire).

</details>

---

## 🤖 Prise en charge des modèles

> Pour les détails, veuillez vous référer à [Documentation de l'API - Interface de passerelle](https://docs.newapi.pro/en/docs/api)

| Type de modèle | Description | Documentation |
|---------|------|------|
| 🤖 OpenAI-Compatible | Modèles compatibles OpenAI | [Documentation](https://docs.newapi.pro/en/docs/api/ai-model/chat/openai/createchatcompletion) |
| 🤖 OpenAI Responses | Format OpenAI Responses | [Documentation](https://docs.newapi.pro/en/docs/api/ai-model/chat/openai/createresponse) |
| 🎨 Midjourney-Proxy | [Midjourney-Proxy(Plus)](https://github.com/novicezk/midjourney-proxy) | [Documentation](https://doc.newapi.pro/api/midjourney-proxy-image) |
| 🎵 Suno-API | [Suno API](https://github.com/Suno-API/Suno-API) | [Documentation](https://doc.newapi.pro/api/suno-music) |
| 🔄 Rerank | Cohere, Jina | [Documentation](https://docs.newapi.pro/en/docs/api/ai-model/rerank/creatererank) |
| 💬 Claude | Format Messages | [Documentation](https://docs.newapi.pro/en/docs/api/ai-model/chat/createmessage) |
| 🌐 Gemini | Format Google Gemini | [Documentation](https://docs.newapi.pro/en/docs/api/ai-model/chat/gemini/geminirelayv1beta) |
| 🔧 Dify | Mode ChatFlow | - |
| 🎯 Amont personnalisé | Configuration des points d'accès amont légalement autorisés | - |

### 📡 Interfaces prises en charge

<details>
<summary>Voir la liste complète des interfaces</summary>

- [Interface de discussion (Chat Completions)](https://docs.newapi.pro/en/docs/api/ai-model/chat/openai/createchatcompletion)
- [Interface de réponse (Responses)](https://docs.newapi.pro/en/docs/api/ai-model/chat/openai/createresponse)
- [Interface d'image (Image)](https://docs.newapi.pro/en/docs/api/ai-model/images/openai/post-v1-images-generations)
- [Interface audio (Audio)](https://docs.newapi.pro/en/docs/api/ai-model/audio/openai/create-transcription)
- [Interface vidéo (Video)](https://docs.newapi.pro/en/docs/api/ai-model/videos/sora/createvideo)
- [Interface d'incorporation (Embeddings)](https://docs.newapi.pro/en/docs/api/ai-model/embeddings/createembedding)
- [Interface de rerank (Rerank)](https://docs.newapi.pro/en/docs/api/ai-model/rerank/creatererank)
- [Conversation en temps réel (Realtime)](https://docs.newapi.pro/en/docs/api/ai-model/realtime/createrealtimesession)
- [Discussion Claude](https://docs.newapi.pro/en/docs/api/ai-model/chat/createmessage)
- [Discussion Google Gemini](https://docs.newapi.pro/en/docs/api/ai-model/chat/gemini/geminirelayv1beta)

</details>

---

## 🚢 Déploiement

Utilisez la [configuration Compose maintenue](docker-compose.yml) et le [guide d'installation](docs/installation/DOCKER.md). Le développement frontend utilise [docker-compose.dev.yml](docker-compose.dev.yml) ; pour un panneau, consultez [les notes BT](docs/installation/BT.md). Les images New-API en amont ne contiennent pas les modifications de cette copie.

Ren2Hub gère la concurrence dans un seul processus. Attendez la fin des requêtes et l'arrêt de l'ancien processus avant de démarrer le nouveau, conformément à la [procédure de publication et de retour arrière](docs/routing/GATEWAY_RELEASE.md). Conservez les volumes de PostgreSQL, Redis et de l'application ; n'utilisez pas de réplicas en mise à jour progressive.

Consultez [.env.example](.env.example) pour les variables, le [contrat des sessions](docs/authentication.md) pour l'authentification, et l'[API de la passerelle](docs/routing/GATEWAY_API.md) et le [routage unifié](docs/routing/UNIFIED_ROUTING.md) pour le FIFO et le budget partagé. Redis conserve les quotas Provider, la fréquence, la santé et le cache ; il ne gère plus la concurrence des utilisateurs et des canaux.

---

## 🔗 Projets connexes

### Projets en amont

| Projet | Description |
|------|------|
| [One API](https://github.com/songquanpeng/one-api) | Base du projet original |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Prise en charge de l'interface Midjourney |

### Outils d'accompagnement

| Projet | Description |
|------|------|
| [new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool) | Outil de recherche de quota d'utilisation avec une clé |
| [new-api-horizon](https://github.com/Calcium-Ion/new-api-horizon) | Version optimisée haute performance de New API |

---

## 💬 Aide et support

### 📖 Ressources de documentation

| Ressource | Lien |
|------|------|
| 📘 FAQ | [FAQ](https://docs.newapi.pro/en/docs/support/faq) |
| 💬 Interaction avec la communauté | [Canaux de communication](https://docs.newapi.pro/en/docs/support/community-interaction) |
| 🐛 Commentaires sur les problèmes | [Commentaires sur les problèmes](https://docs.newapi.pro/en/docs/support/feedback-issues) |
| 📚 Documentation complète | [Documentation officielle](https://docs.newapi.pro/en/docs) |

### 🤝 Guide de contribution

Bienvenue à toutes les formes de contribution!

- 🐛 Signaler des bogues
- 💡 Proposer de nouvelles fonctionnalités
- 📝 Améliorer la documentation
- 🔧 Soumettre du code

---

## 📜 Licence

Ce projet est sous licence [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE).

Il s'agit d'un projet open-source développé sur la base de [One API](https://github.com/songquanpeng/one-api) (licence MIT).

Si les politiques de votre organisation ne permettent pas l'utilisation de logiciels sous licence AGPLv3, ou si vous souhaitez éviter les obligations open-source de l'AGPLv3, veuillez nous contacter à : [support@quantumnous.com](mailto:support@quantumnous.com)

---

## 🌟 Historique des étoiles

<div align="center">

[![Graphique de l'historique des étoiles](https://api.star-history.com/svg?repos=Calcium-Ion/new-api&type=Date)](https://star-history.com/#Calcium-Ion/new-api&Date)

</div>

---

<div align="center">

### 💖 Merci d'utiliser New API

Si ce projet vous est utile, bienvenue à nous donner une ⭐️ Étoile！

**[Documentation officielle](https://docs.newapi.pro/en/docs)** • **[Commentaires sur les problèmes](https://github.com/Calcium-Ion/new-api/issues)** • **[Dernière version](https://github.com/Calcium-Ion/new-api/releases)**

<sub>Construit avec ❤️ par QuantumNous</sub>

</div>
