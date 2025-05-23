---
title: Supported admin APIs
parent: Administration
nav_order: 4
permalink: /administration/adminapi
---

# Supported admin APIs

Dendrite supports, at present, a very small number of endpoints that allow
admin users to perform administrative functions. Please note that there is no
API stability guarantee on these endpoints at present — they may change shape
without warning.

More endpoints will be added in the future.

Endpoints may be used directly through curl:

```
curl --header "Authorization: Bearer <access_token>" -X <POST|GET|PUT> <Endpoint URI> -d '<Request Body Contents>'
```

An `access_token` can be obtained through most Element-based matrix clients by going to `Settings` -> `Help & About` -> `Advanced` -> `Access Token`.
Be aware that an `access_token` allows a client to perform actions as an user and should be kept **secret**.

The user must be an administrator in the `userapi_accounts` table in order to use these endpoints.

Existing user accounts can be set to administrative accounts by changing `account_type` to `3` in `userapi_accounts`

```
UPDATE userapi_accounts SET account_type = 3 WHERE localpart = '$localpart';
```

Where `$localpart` is the username only (e.g. `alice`).

## POST `/_dendrite/admin/evacuateRoom/{roomID}`

This endpoint will instruct Dendrite to part all local users from the given `roomID`
in the URL. It may take some time to complete. A JSON body will be returned containing
the user IDs of all affected users.

If the room has an alias set (e.g. is published), the room's ID will not be visible in the URL, but it can
be found as the room's "internal ID" in Element Web (Settings -> Advanced)

## POST `/_dendrite/admin/evacuateUser/{userID}`

This endpoint will instruct Dendrite to part the given local `userID` in the URL from
all rooms which they are currently joined. A JSON body will be returned containing
the room IDs of all affected rooms.

## POST `/_dendrite/admin/resetPassword/{userID}`

Reset the password of a local user. 

**If `logout_devices` is set to `true`, all `access_tokens` will be invalidated, resulting
in the potential loss of encrypted messages**

Request body format:

```json
{
    "password": "new_password_here",
    "logout_devices": false
}
```

## GET `/_dendrite/admin/fulltext/reindex`

This endpoint instructs Dendrite to reindex all searchable events (`m.room.message`, `m.room.topic` and `m.room.name`). An empty JSON body will be returned immediately.
Indexing is done in the background, the server logs every 1000 events (or below) when they are being indexed. Once reindexing is done, you'll see something along the lines `Indexed 69586 events in 53.68223182s` in your debug logs.

## POST `/_dendrite/admin/refreshDevices/{userID}`

This endpoint instructs Dendrite to immediately query `/devices/{userID}` on a federated server. An empty JSON body will be returned on success, updating all locally stored user devices/keys. This can be used to possibly resolve E2EE issues, where the remote user can't decrypt messages.

## POST `/_dendrite/admin/purgeRoom/{roomID}`

This endpoint instructs Dendrite to remove the given room from its database. It does **NOT** remove media files. Depending on the size of the room, this may take a while. Will return an empty JSON once other components were instructed to delete the room.

## POST `/_synapse/admin/v1/send_server_notice`

Request body format:
```json
{
    "user_id": "@target_user:server_name",
    "content": {
       "msgtype": "m.text",
       "body": "This is my message"
    }
}
```

Send a server notice to a specific user. See the [Matrix Spec](https://spec.matrix.org/v1.3/client-server-api/#server-notices) for additional details on server notice behaviour.
If successfully sent, the API will return the following response:

```json
{
     "event_id": "<event_id>"
}
```

## GET `/_synapse/admin/v1/register`

Shared secret registration — please see the [user creation page](1_createusers.md) for
guidance on configuring and using this endpoint.

## GET `/_matrix/client/v3/admin/whois/{userId}`

From the [Matrix Spec](https://spec.matrix.org/v1.3/client-server-api/#get_matrixclientv3adminwhoisuserid). 
Gets information about a particular user. `userId` is the full user ID (e.g. `@alice:domain.com`)
