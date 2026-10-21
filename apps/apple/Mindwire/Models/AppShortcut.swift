import Foundation
import SwiftUI

struct AppShortcutIcon: View {
    let shortcut: AppShortcut
    var size: CGFloat = 48
    var body: some View { SavedAppIcon(id: shortcut.id, icon: shortcut.icon, size: size) }
}

/// A computer-owned General-mode shortcut. This is a metadata cache, not an app runtime.
struct AppShortcut: Codable, Identifiable, Hashable {
    let id: String
    var workspaceId: String
    let projectId: String
    var botId: String?
    var name: String
    var icon: String?
    var url: String
    var startupCommand: String?
    var healthPath: String?
    var archived: Bool
    let createdAt: String
    var revision: Int64
    var channelIds: [String]? = nil
    var notificationsMuted: Bool? = nil
    var automation: Bool? = nil
    var staticDirectory: String? = nil
    var publicUrl: String? = nil

    var publicLink: URL? {
        guard let publicUrl, let url = URL(string: publicUrl), url.scheme == "https", url.host != nil,
              url.user == nil, url.password == nil else { return nil }
        return url
    }
    var shareLink: URL? { publicLink ?? destination }

    var destination: URL? {
        guard let value = URL(string: url), ["http", "https"].contains(value.scheme?.lowercased() ?? ""),
              value.host != nil, value.user == nil, value.password == nil else { return nil }
        return value
    }
    var isLocalLink: Bool { destination?.host.map { ["localhost", "127.0.0.1", "::1", "[::1]"].contains($0.lowercased()) } ?? false }
    var needsComputer: Bool { isLocalLink || !(startupCommand ?? "").isEmpty }
}

struct AppShortcutMutation: Decodable {
    let app: AppShortcut
    let snapshot: WorkspaceMetadata
}

struct AppShortcutEdit: Encodable {
    let expectedRevision: Int64
    var name: String? = nil
    var icon: String? = nil
    var url: String? = nil
    var archived: Bool? = nil
    var channelIds: [String]? = nil
    var notificationsMuted: Bool? = nil
    var staticDirectory: String? = nil
    var publicUrl: String? = nil
}

struct AppPublishRequest: Decodable { let app: AppShortcut }
struct AppPublicationPlan: Decodable {
    let app: AppShortcut
    let kind: String
    let port: Int?
    let directory: String?
}

struct AppNotificationChannel: Decodable, Identifiable { let id: String; let name: String; let type: String }

struct AppSchedule: Codable, Identifiable {
    let id: String
    let appId: String?
    var chatId: String?
    var prompt: String?
    var lastRunId: String?
    var name: String
    var runAt: String?
    var intervalSeconds: Int?
    var localTime: String?
    var timeZone: String?
    var weekdays: [Int]?
    var command: String?
    var title: String?
    var body: String?
    var revision: Int64
    var active: Bool
    var nextRunAt: String?
    var retryAt: String?
    var lastRunAt: String?
    var state: String
    var lastError: String?

    func edit(enabled: Bool) -> [String: Any] {
        var result: [String: Any] = ["id": id, "expectedRevision": revision, "name": name, "enabled": enabled]
        if let runAt, !runAt.isEmpty { result["runAt"] = runAt }
        if let intervalSeconds, intervalSeconds > 0 { result["intervalSeconds"] = intervalSeconds }
        if let localTime, !localTime.isEmpty { result["localTime"] = localTime }
        if let timeZone { result["timeZone"] = timeZone }
        if let weekdays { result["weekdays"] = weekdays }
        if let command { result["command"] = command }
        if let title { result["title"] = title }
        if let body { result["body"] = body }
        if let prompt { result["prompt"] = prompt }
        return result
    }
}

typealias SavedSchedule = AppSchedule
