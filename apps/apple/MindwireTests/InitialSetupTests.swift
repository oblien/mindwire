import XCTest
@testable import Mindwire

@MainActor
final class InitialSetupTests: XCTestCase {
    private func defaults() throws -> UserDefaults {
        let name = "initial-setup-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        addTeardownBlock { defaults.removePersistentDomain(forName: name) }
        return defaults
    }

    func testChoosingEitherModeDoesNotUnlockAnUnfinishedInstallAfterRelaunch() throws {
        for mode in AppExperienceMode.allCases {
            let defaults = try defaults()
            let first = AppExperience(defaults: defaults, hasExistingData: false)
            first.mode = mode
            first.pendingWorkspaceID = "paired-computer"
            first.pendingAgentID = "unfinished-profile"
            XCTAssertTrue(first.needsInitialSetup)
            let relaunched = AppExperience(defaults: defaults, hasExistingData: true)
            XCTAssertTrue(relaunched.needsInitialSetup)
            XCTAssertEqual(relaunched.mode, mode)
            XCTAssertEqual(relaunched.pendingWorkspaceID, "paired-computer")
            XCTAssertEqual(relaunched.pendingAgentID, "unfinished-profile")
        }
    }

    func testSavedConnectionsAloneDoNotCompleteMigrationButExistingChatsRemainAvailable() throws {
        let incomplete = AppExperience(defaults: try defaults(), hasExistingData: true)
        XCTAssertTrue(incomplete.needsInitialSetup)
        let returning = AppExperience(defaults: try defaults(), hasExistingData: true, hasExistingConversation: true)
        XCTAssertFalse(returning.needsInitialSetup)
        XCTAssertEqual(returning.mode, .developer)
    }

    func testOnlyMatchingAgentAndProjectFinishSetupAndPreserveTheirIDs() throws {
        let defaults = try defaults()
        let experience = AppExperience(defaults: defaults, hasExistingData: false)
        experience.mode = .general
        experience.pendingWorkspaceID = "computer"
        experience.pendingAgentID = "agent"
        let agent = Agent(id: "agent", name: "Mindwire", agentType: .codex, workspaceId: "computer", createdAt: "now")
        let wrong = Project(id: "wrong", name: "Other", workspaceId: "other", path: "/other", createdAt: "now")
        experience.use(agent: agent, project: wrong)
        XCTAssertTrue(experience.needsInitialSetup)
        XCTAssertNil(experience.assistantAgentID)
        let project = Project(id: "project", name: "Mindwire", workspaceId: "computer", path: "/home/me/.mindwire-workspace", createdAt: "now")
        experience.use(agent: agent, project: project)
        XCTAssertFalse(experience.needsInitialSetup)
        XCTAssertNil(experience.pendingWorkspaceID)
        XCTAssertNil(experience.pendingAgentID)
        let relaunched = AppExperience(defaults: defaults, hasExistingData: true)
        XCTAssertFalse(relaunched.needsInitialSetup)
        XCTAssertEqual(relaunched.assistantAgentID, "agent")
        XCTAssertEqual(relaunched.projectIDs["computer"], "project")
        relaunched.mode = .developer
        XCTAssertFalse(relaunched.needsInitialSetup)
        XCTAssertEqual(relaunched.assistantAgentID, "agent")
    }
}
