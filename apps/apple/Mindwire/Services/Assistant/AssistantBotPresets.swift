import CryptoKit
import Foundation

/// Consumer defaults belong to the client. The daemon stores ordinary bot
/// profiles and grants no permissions based on these names, IDs or prompts.
enum AssistantBotPresets {
    static func appBuilderID(projectId: String) -> String {
        let digest = SHA256.hash(data: Data(projectId.utf8)).prefix(12)
        return "apps-" + digest.map { String(format: "%02x", $0) }.joined()
    }

    static let assistantInstructions = """
    You are Mindwire, the user's personal assistant on their phone and Mac. Help with everyday work, documents, research and actions on connected computers. Be concise, use their language and handle technical details yourself. Reuse their connected AI subscriptions and tools.

    Help directly or delegate through the saved bot tools. Bots retain separate identity, memory and conversations. Use the assigned skills for app-building, reminders and background work; delegate app creation to the App builder when useful. Report only verified results and respect the user's approvals.
    """

    static let appBuilderInstructions = """
    Help the user turn ideas into useful working apps using their existing tools and AI connection. Handle technical setup and explain results briefly. Use the assigned app-building and automation skills when the task calls for them. Respect user choices, existing apps and required approvals.
    """

    // Exact fingerprints of shipped defaults. User-edited instructions remain intact.
    private static let legacyAssistant = "b924a63ee87a23ace108c81d28c9013da58439cf626a410bfde2b5de7cded6b5"
    private static let legacyBuilder = "b4f15e3feb8827f70b0f1e282842f8c2481ecaec60457f6f83c7bddeeb7a464a"
    private static let legacyAutomationFallbacks: Set<String> = [
        "cbab6c9b381f57621a4f3ebbabcacb7eadda4f0c574dae27a6d31b92c9fb18b6",
        "feb42ae917bcf41c67d8022c1f3380f0f402152f5083b7bea7b5dbd3a889b3e0",
    ]
    private static let documents: Result<[String], Error> = Result {
        try ["mindwire-apps", "mindwire-automations"].map { name in
            guard let url = Bundle.main.url(forResource: "SKILL", withExtension: "md", subdirectory: "BotSkills/" + name) else {
                throw CocoaError(.fileNoSuchFile)
            }
            return try String(contentsOf: url, encoding: .utf8)
        }
    }

    static func appBuilder(agentId: String, projectId: String, name: String = "App builder", supportsSkills: Bool) throws -> AssistantBotCreate {
        let workflows = try documents.get()
        return .init(agentId: agentId, projectId: projectId, name: name,
                     id: appBuilderID(projectId: projectId), icon: "peach-robot",
                     purpose: "Build useful apps and save them to Apps.",
                     instructions: instructions(appBuilderInstructions, workflows: workflows, supportsSkills: supportsSkills),
                     skills: supportsSkills ? workflows : nil)
    }

    static func profileEdit(_ bot: AssistantBot, supportsSkills: Bool) throws -> AssistantBotEdit? {
        guard bot.isMaster || bot.isAppBuilder else { return nil }
        var edit = AssistantBotEdit(expectedRevision: bot.revision)
        let hash = SHA256.hash(data: Data(bot.instructions.utf8)).map { String(format: "%02x", $0) }.joined()
        let legacy = bot.isMaster ? legacyAssistant : legacyBuilder
        let mindset = bot.isMaster ? assistantInstructions : appBuilderInstructions
        let workflows = try documents.get()
        let fallback = instructions(mindset, workflows: workflows, supportsSkills: false)
        let newManager = bot.isMaster && bot.instructions.isEmpty && bot.skills == nil
        if newManager || hash == legacy || legacyAutomationFallbacks.contains(hash) || bot.instructions == fallback {
            let value = instructions(mindset, workflows: workflows, supportsSkills: supportsSkills)
            if bot.instructions != value { edit.instructions = value }
        }
        // An explicit skill assignment, including an empty list, is user-owned.
        if supportsSkills, bot.skills == nil { edit.skills = workflows }
        if supportsSkills, let assigned = bot.skills {
            // Upgrade only an exact, previously shipped workflow. Custom skills
            // and explicit removals remain user-owned.
            let updated = assigned.map { source -> String in
                let digest = SHA256.hash(data: Data(source.utf8)).map { String(format: "%02x", $0) }.joined()
                if digest == "0e759564d539b6d8eedd8e3bdfcecf898cf9c277e4a98fdd30d4a2aada2b2239" { return workflows[0] }
                if digest == "ad5ea09c7c7da4240c487084dfb352cbc298dc90e08958a8b99bce32532ed47c" { return workflows[1] }
                return source
            }
            if updated != assigned { edit.skills = updated }
        }
        return edit.instructions == nil && edit.skills == nil ? nil : edit
    }

    private static func instructions(_ mindset: String, workflows: [String], supportsSkills: Bool) -> String {
        supportsSkills ? mindset : mindset + "\n\nWorkflow instructions for this connection:\n" + workflows.joined(separator: "\n\n")
    }
}
