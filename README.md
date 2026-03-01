# Création de code avec oapi

On utilise un exemple minimaliste.  
On utilise oapi-codegen avec un fichier yaml de configuration.
On vibecode aver parcimonie et relecture.  

## Pré-requis

Doivent être installés :
* go: voir https://go.dev/doc/install
* oapi-codegen: voir https://github.com/oapi-codegen/oapi-codegen (noter la différence d'installation entre Go 1.24+ et version précédentes)

## Utilisation

```bash
mkdir -p src/internal/gen
oapi-codegen -config oapi-models.yaml openapi.yaml
oapi-codegen -config oapi-server.yaml openapi.yaml
cd src
go mod init notes-api
go mod tidy
go run ./cmd/api # to run the API
```

## Architecture type

```
HTTP → handlers → service → repository → DB
```

* `/cmd/api/main.go`: Point d’entrée de l’application. Juste du câblage. Assembler les briques, puis disparaître.
    * Responsabilités
        * Charger la config (env, flags)
        * Initialiser la DB
        * Créer les repositories
        * Créer les services
        * Créer les handlers
        * Lancer le serveur HTTP
    * Ne doit pas faire
        * De la logique métier
        * Des requêtes SQL
        * Du traitement HTTP
* `/internal/gen/`: Code généré par oapi-codegen à partir de la spec OpenAPI. Le contrat entre la spec API et le code.
    * Responsabilités
        * Définir les modèles API (requêtes/réponses)
        * Définir l’interface serveur
        * Fournir les helpers de routing (Chi, etc.)
    * Ne doit pas faire
        * Être modifié à la main
        * Contenir de la logique métier
* `/internal/api/`: Couche HTTP. Traduit HTTP ↔ application. JSON → struct → service → struct → JSON
    * Responsabilités
        * Lire la requête (body, params, headers)
        * Valider les entrées (niveau syntaxe)
        * Appeler le service
        * Convertir le résultat → réponse HTTP
    * Ne doit pas faire
        * De logique métier
        * Accéder directement à la DB   
* `/internal/service/`: Le cœur de l'application. Ce que fait réellement l'application.
    * Responsabilités
        * Implémenter les cas d’usage
        * Appliquer les règles métier
        * Orchestrer les repositories
    * Ne doit pas faire
        * Du HTTP
        * Du SQL  
* `/internal/repository/`: Couche de persistance. Comment les données sont stockées et récupérées.
    * Responsabilités
        * Exécuter les requêtes SQL
        * Mapper les résultats → structs
        * Retourner les données au service
    * Ne doit pas faire
        * Du HTTP
        * De logique métier
* `/internal/db/`: Initialisation de la base de données. Infrastructure.
    * Responsabilités
        * Ouvrir la connexion
        * Configurer le pool
        * Vérifier la connexion
    * Ne doit pas faire
        * Des requêtes SQL
        * De logique métier

## Architecture type avec un ORM (GORM)

Voir dans src-gorm. 
* On ajoute `/internal/model` pour stocker les modèles utilisés par GORM.
* On réécrit `/internal/db`  et `/internal/repository` pour utiliser GORM plutôt que de faire des requêtes SQL
