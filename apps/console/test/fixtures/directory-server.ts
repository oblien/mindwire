// Starts the real entrypoint. Test controls use child IPC, never HTTP routes.
import { createHmac } from "node:crypto";
import { Pool } from "pg";
import { authDatabase } from "../../server/database";
import { env } from "../../server/env";
import { issueChallenge } from "../../server/computer-directory/challenge";
import { sourceNetwork } from "../../server/computer-directory/admission";
import { DirectoryStore } from "../../server/computer-directory/store";
import { seedLegacyDirectory } from "./legacy-directory";

await seedLegacyDirectory();
await import("../../server/index");
const { auth } = await import("../../server/auth");
const { getSession } = await import("../../server/session");
const { pruneDirectory } = await import("../../server/computer-directory/routes");

process.on("message", async (message: { id: number; action: string; value?: any }) => {
  try {
    let value: unknown;
    switch (message.action) {
      case "hasFleet":
        value = !!getSession(message.value);
        break;
      case "deleteUser":
        await (await auth.$context).internalAdapter.deleteUser(message.value);
        value = true;
        break;
      case "accountCount":
        value = await (await auth.$context).adapter.count({ model: "user" });
        break;
      case "challenge": {
        const network = createHmac("sha256", env.authSecret)
          .update("mindwire-directory-source-v1\n" + sourceNetwork(message.value.address ?? "127.0.0.1"))
          .digest("base64url");
        value = issueChallenge(message.value.directoryId, network, message.value.timestamp);
        break;
      }
      case "enrollmentBudget": {
        const query = "SELECT source,day,count FROM computer_directory_enrollment_budget";
        value =
          authDatabase instanceof Pool
            ? (await authDatabase.query(query)).rows
            : authDatabase.prepare(query).all();
        break;
      }
      case "expirePublication":
        if (authDatabase instanceof Pool)
          await authDatabase.query("UPDATE computer_directory SET expires_at=0 WHERE directory_id=$1", [
            message.value,
          ]);
        else
          authDatabase
            .prepare("UPDATE computer_directory SET expires_at=0 WHERE directory_id=?")
            .run(message.value);
        value = true;
        break;
      case "prune":
        await pruneDirectory(message.value);
        value = true;
        break;
      case "row": {
        const row = await new DirectoryStore().get(message.value);
        value = row
          ? { sequence: row.sequence, hasPublication: !!row.publication, revoked: !!row.revoked }
          : null;
        break;
      }
      default:
        throw new Error("Unknown fixture command.");
    }
    process.send?.({ id: message.id, value });
  } catch {
    process.send?.({ id: message.id, error: true });
  }
});
