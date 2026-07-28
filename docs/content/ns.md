---
title: "The x: namespace"
description: "Every term in the x: RDF vocabulary, with a one-line definition. The namespace URL x rdf writes, dereferenced."
weight: 95
---

```turtle
@prefix x: <https://x-cli.tamnd.com/ns#> .
```

`x rdf` writes schema.org wherever a term exists and `x:` where none does. This
page is the `x:` half. It is served at the namespace URL itself, so a triple
store that follows its nose from `x:repliesTo` lands here.

Nothing on this page is an invention where schema.org already had a word. A
reply is not a `schema:comment`, because a comment is a different thing on a
different kind of page; a repost is not a `schema:sharedContent`, because that
is what was shared and not the act of sharing. Where the fit was close enough,
the mapping uses schema.org and this page says nothing at all: `schema:author`,
`schema:mentions`, `schema:citation`, `schema:keywords`,
`schema:associatedMedia`, `schema:sameAs`, `schema:location`,
`schema:containedInPlace`, `schema:memberOf`.

## Classes

| Term | What it is |
|---|---|
| `x:Conversation` | A thread, identified by the id X gives the whole conversation rather than any one post in it. |
| `x:Hashtag` | A hashtag, lowercased, without the `#`. |
| `x:Cashtag` | A ticker symbol tag, lowercased, without the `$`. |
| `x:Trend` | One trending topic, in one place, at one moment. All three parts are part of the identity. |
| `x:Poll` | A poll attached to a post. |
| `x:Card` | A link preview card: the title, description, and image X renders for a URL. |
| `x:CommunityNote` | A Community Note attached to a post. |

## Properties

| Term | Domain to range | What it says |
|---|---|---|
| `x:repliesTo` | post to post | This post is a reply to that one. |
| `x:reposts` | post to post | This post is a repost of that one. |
| `x:inConversation` | post to conversation | This post belongs to that thread. |
| `x:taggedSymbol` | post to cashtag | This post carries that ticker tag. |
| `x:hasPoll` | post to poll | This post carries that poll. |
| `x:hasCard` | post to card | This post carries that link preview. |
| `x:hasNote` | post to note | This post carries that Community Note. |
| `x:pinned` | account to post | This account has pinned that post to its profile. |
| `x:follows` | account to account | The first account follows the second. A followers listing and a following listing both normalise to this, pointing the way the claim points. |
| `x:liked` | account to post | This account liked that post. |
| `x:reposted` | account to post | This account reposted that post. |
| `x:owns` | account to list | This account owns that list. |
| `x:hosted` | account to space | This account hosted that Space, or created it. |
| `x:spokeIn` | account to space | This account held the microphone in that Space. Not `x:hosted`, because a speaker is not an admin. |
| `x:joined` | account to dateTime | When the account was created. `schema:dateCreated` describes the page rather than the person, so this is its own term. |
| `x:state` | space to text | X's own word for what became of a Space: Running, Scheduled, Ended, TimedOut, Canceled. Not normalised, because a Space that timed out and one the host ended are different events and only X knows which happened. |

## Counters

Engagement counts are not `x:` terms. They are `schema:InteractionCounter`,
which is what X itself publishes in the microdata on every status and profile
page, so the counts a crawl produces line up with the counts X states:

```turtle
<x://tweet/20> schema:interactionStatistic [
  a schema:InteractionCounter ;
  schema:interactionType schema:LikeAction ;
  schema:name "Likes" ;
  schema:userInteractionCount 307403
] .
```

Followers use `schema:interactionStatistic` and follows use
`schema:agentInteractionStatistic`, which is the same split X makes: one is
something done to the account, the other is something the account did.
