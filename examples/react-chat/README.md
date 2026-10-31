# Mindwire React chat

A complete chat using `@raptor-ai/react` and its included Mindwire adapter.
It connects to a running daemon; the browser does not start an agent runtime.

1. Start Mindwire on your computer and authenticate the agent you want to use.
2. In this directory, copy `.env.example` to `.env.local`. Set `MINDWIRE_URL`,
   `MINDWIRE_TOKEN` when required, and `VITE_MINDWIRE_CWD` to a folder on that computer.
3. Run `npm install` and `npm run dev`.
4. Open the printed localhost URL. Add `?chat=my-chat` to use a different conversation.

`MINDWIRE_TOKEN` is attached by the Vite development proxy and is not exposed as
a browser environment variable. This proxy is for local development. In your
deployed application, use your own authenticated proxy and authorize each
user's access to the selected computer and conversation.

The example includes streaming text, thinking, tool activity, paged history,
file attachments, cancellation, reconnect and agent approval/question forms.
Sending a message while the agent works is enabled only when that run supports it.
Voice controls are disabled because native voice uses a separate protocol.

Run `npm run build` to check TypeScript and build the browser application.
The [React integration guide](https://mindwire.sh/docs/guides/react) explains the
adapter options and how to customize the components.
