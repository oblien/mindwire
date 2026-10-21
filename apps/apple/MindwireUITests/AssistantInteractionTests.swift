#if os(iOS)
import XCTest

@MainActor
final class AssistantInteractionTests: XCTestCase {
    override func setUpWithError() throws { continueAfterFailure = false }

    func testFirstRunKeepsConnectionAlternativesSeparateAndErrorsInline() {
        for theme in ["light", "dark"] {
            let app = launch("onboarding", theme: theme)
            let start = app.buttons["onboarding.start"]
            XCTAssertTrue(start.waitForExistence(timeout: 15), app.debugDescription)
            XCTAssertTrue(start.isHittable)
            XCTAssertFalse(app.tabBars.firstMatch.exists)
            XCTAssertFalse(app.buttons["assistant.sidebar.open"].exists)
            XCTAssertFalse(app.descendants(matching: .any)["chat.composer"].firstMatch.exists)
            capture("New welcome — \(theme)")
            start.tap()
            let own = app.buttons["assistant.choose.computer"]
            let cloud = app.buttons["assistant.choose.cloud"]
            XCTAssertTrue(own.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(cloud.isHittable)
            XCTAssertFalse(app.buttons["Cancel"].exists, "First setup cannot be dismissed into an unconnected app")
            capture("Choose where your bot works — \(theme)")
            own.tap()
            XCTAssertTrue(app.buttons["Add a computer"].waitForExistence(timeout: 5))
            app.buttons["Add a computer"].tap()
            XCTAssertTrue(app.buttons["computer-add-scan"].waitForExistence(timeout: 5))
            capture("Pairing instructions — \(theme)")
            XCTAssertFalse(app.buttons["onboarding.pair.copy"].exists, "Installation should not compete with the default pairing instruction")
            XCTAssertFalse(app.buttons["onboarding.pair.paste"].exists, "Manual links are disclosed only when needed")
            app.buttons["Install or connect another way"].tap()
            app.buttons["onboarding.pair.install"].tap()
            XCTAssertTrue(app.buttons["onboarding.pair.copy"].waitForExistence(timeout: 5))
            capture("Install and pair — \(theme)")
            app.buttons["Back to QR setup"].tap()
            XCTAssertTrue(app.buttons["computer-add-scan"].waitForExistence(timeout: 5))
            app.buttons["onboarding.pair.paste"].tap()
            let field = app.textFields["onboarding.pair.link"]
            XCTAssertTrue(field.waitForExistence(timeout: 5), app.debugDescription)
            field.tap()
            field.typeText("not-a-connection")
            app.buttons["onboarding.pair.connect"].tap()
            let error = app.staticTexts["This isn't a valid Mindwire connection code. Scan a new code from your computer."]
            XCTAssertTrue(error.waitForExistence(timeout: 10), app.debugDescription)
            XCTAssertFalse(app.alerts.firstMatch.exists, "Pairing errors stay in context")
            XCTAssertFalse(app.descendants(matching: .any)["chat.composer"].firstMatch.exists)
            capture("Recover from an invalid pairing link — \(theme)")
            app.buttons["Back to QR setup"].tap()
            XCTAssertTrue(app.buttons["computer-add-scan"].waitForExistence(timeout: 5))
            app.terminate()
        }
    }

    func testOnboardingSupportsAccessibilityTextWithoutHidingContinue() {
        let app = launch("onboarding", theme: "dark", largeText: true)
        let next = app.buttons["onboarding.start"]
        XCTAssertTrue(next.waitForExistence(timeout: 15))
        XCTAssertTrue(next.isHittable)
        capture("Welcome — accessibility text")
        next.tap()
        let cloud = app.buttons["assistant.choose.cloud"]
        for _ in 0..<6 where !cloud.isHittable { app.swipeUp() }
        XCTAssertTrue(cloud.isHittable)
        XCTAssertFalse(app.tabBars.firstMatch.exists)
        capture("Setup choices — accessibility text")
        cloud.tap()
        XCTAssertTrue(app.navigationBars["Mindwire Cloud"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["assistant.sidebar.open"].exists)
        app.terminate()
    }

    func testModeSettingsCanPreviewCancelAndThenSwitch() {
        let app = launch("mode-settings", theme: "light")
        let mode = app.buttons["settings.appMode"]
        XCTAssertTrue(mode.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertTrue(mode.label.contains("Developer"))
        mode.tap()
        let picker = app.segmentedControls["experience.mode"]
        XCTAssertTrue(picker.waitForExistence(timeout: 5))
        picker.buttons["General"].tap()
        app.buttons["experience.cancel"].tap()
        XCTAssertTrue(mode.waitForExistence(timeout: 5))
        XCTAssertTrue(mode.label.contains("Developer"), "Cancel must leave the current mode intact")
        mode.tap()
        picker.buttons["General"].tap()
        app.buttons["experience.continue"].tap()
        XCTAssertTrue(app.buttons["assistant.sidebar.open"].waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(app.descendants(matching: .any)["chat.composer"].firstMatch.exists,
                      "Switching modes must preserve the existing connection and conversation")
        app.terminate()
    }

    func testSidebarSearchAndClosingPreserveTheCurrentDraft() {
        for theme in ["light", "dark"] {
            let app = launch("assistant", theme: theme, content: "bots-history")
            let composer = app.descendants(matching: .any)["chat.composer"].firstMatch
            XCTAssertTrue(composer.waitForExistence(timeout: 15), app.debugDescription)
            XCTAssertFalse(app.navigationBars.buttons["New chat"].exists)
            composer.tap()
            composer.typeText("Keep my unfinished message")
            app.buttons["assistant.sidebar.open"].tap()
            let search = app.textFields["assistant.sidebar.search"]
            XCTAssertTrue(search.waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertFalse(app.buttons["assistant.sidebar.open"].exists,
                           "The chat toolbar must not leave a second sidebar toggle on the exposed edge")
            XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 5))
            XCTAssertTrue(app.buttons["assistant.sidebar.options"].isHittable)
            let exposedChat = app.buttons["assistant.sidebar.backdrop"]
            XCTAssertTrue(exposedChat.exists, app.debugDescription)
            XCTAssertGreaterThan(exposedChat.frame.minX, app.frame.width * 0.6,
                                 "Opening the drawer must move the chat body aside, not cover it in place")
            XCTAssertEqual(exposedChat.frame.maxX, app.frame.maxX, accuracy: 1)
            let settledSearchFrame = search.frame
            let settledOverlayFrame = exposedChat.frame
            for _ in 0..<5 {
                Thread.sleep(forTimeInterval: 0.2)
                XCTAssertEqual(search.frame.minX, settledSearchFrame.minX, accuracy: 1,
                               "The open drawer must remain still")
                XCTAssertEqual(exposedChat.frame.minX, settledOverlayFrame.minX, accuracy: 1)
            }
            capture("General sidebar — \(theme)")

            // A vertical history scroll moves its header while the exposed chat stays put.
            let history = app.descendants(matching: .any)["chat.history.list"].firstMatch
            history.swipeUp()
            XCTAssertTrue(exposedChat.isHittable)
            XCTAssertEqual(exposedChat.frame.minX, settledOverlayFrame.minX, accuracy: 1)
            XCTAssertTrue(!search.isHittable || search.frame.minY < settledSearchFrame.minY - 30,
                          "Logo and search must scroll with the history")
            capture("Scrolled General sidebar — \(theme)")
            history.swipeDown()
            XCTAssertTrue(search.isHittable)

            search.tap()
            search.typeText("Polish the onboarding")
            XCTAssertTrue(app.buttons["chat.history.saved.store-project-0-chat-1"].waitForExistence(timeout: 5))
            XCTAssertFalse(app.buttons["chat.history.saved.store-project-0-chat-0"].exists)
            app.buttons["assistant.sidebar.clearSearch"].tap()
            let currentChat = app.buttons["chat.history.saved.store-project-0-chat-0"]
            XCTAssertTrue(currentChat.waitForExistence(timeout: 5))
            currentChat.tap()
            assertSidebarClosed(app)
            XCTAssertEqual(composer.value as? String, "Keep my unfinished message")

            app.buttons["assistant.sidebar.open"].tap()
            exposedChat.tap()
            assertSidebarClosed(app)
            XCTAssertEqual(composer.value as? String, "Keep my unfinished message")
            app.buttons["assistant.sidebar.open"].tap()
            // A slow, short drag snaps back; it must not leave a displaced or shaking panel.
            let heading = app.staticTexts["assistant.sidebar.chats"].coordinate(withNormalizedOffset: .zero)
            let start = heading.withOffset(CGVector(dx: 250, dy: 8))
            start.press(forDuration: 0.05, thenDragTo: start.withOffset(CGVector(dx: -40, dy: 0)),
                        withVelocity: XCUIGestureVelocity(rawValue: 60), thenHoldForDuration: 0.3)
            XCTAssertTrue(search.isHittable)
            XCTAssertEqual(search.frame.minX, settledSearchFrame.minX, accuracy: 1)
            XCTAssertEqual(exposedChat.frame.minX, settledOverlayFrame.minX, accuracy: 1)
            heading.withOffset(CGVector(dx: 250, dy: 8)).press(forDuration: 0.05,
                thenDragTo: heading.withOffset(CGVector(dx: 0, dy: 8)))
            assertSidebarClosed(app)
            XCTAssertEqual(composer.value as? String, "Keep my unfinished message")

            // Opening must work from the chat body, beyond the narrow system edge region.
            let bodyStart = app.coordinate(withNormalizedOffset: CGVector(dx: 0.25, dy: 0.5))
            bodyStart.press(forDuration: 0.05, thenDragTo: bodyStart.withOffset(CGVector(dx: 50, dy: 0)),
                       withVelocity: XCUIGestureVelocity(rawValue: 80), thenHoldForDuration: 0.3)
            assertSidebarClosed(app)
            composer.tap()
            bodyStart.press(forDuration: 0.05, thenDragTo: bodyStart.withOffset(CGVector(dx: 260, dy: 0)),
                       withVelocity: XCUIGestureVelocity(rawValue: 400), thenHoldForDuration: 0.1)
            XCTAssertTrue(exposedChat.waitForExistence(timeout: 5))
            XCTAssertTrue(search.isHittable)
            XCTAssertTrue(app.keyboards.firstMatch.waitForNonExistence(timeout: 5))
            XCTAssertFalse(app.buttons["assistant.sidebar.open"].exists)
            XCTAssertEqual(exposedChat.frame.minX, settledOverlayFrame.minX, accuracy: 1)
            capture("Sidebar opened by dragging the chat body — \(theme)")
            exposedChat.tap()
            assertSidebarClosed(app)
            XCTAssertEqual(composer.value as? String, "Keep my unfinished message")
            app.terminate()
        }
    }

    func testEmptyChatKeepsNewChatInTheSidebar() {
        for theme in ["light", "dark"] {
            let app = launch("assistant", theme: theme, content: "empty")
            XCTAssertTrue(app.descendants(matching: .any)["chat.empty"].firstMatch.waitForExistence(timeout: 15))
            XCTAssertFalse(app.navigationBars.buttons["New chat"].exists)
            capture("Empty General chat — \(theme)")
            app.buttons["assistant.sidebar.open"].tap()
            let create = app.buttons["assistant.sidebar.newChat"]
            let search = app.textFields["assistant.sidebar.search"]
            XCTAssertTrue(create.isHittable)
            XCTAssertEqual(create.frame.midY, search.frame.midY, accuracy: 1,
                           "New chat must be an icon beside the search field")
            XCTAssertGreaterThan(create.frame.minX, search.frame.maxX)
            XCTAssertLessThan(create.frame.width, 60)
            XCTAssertFalse(app.buttons["assistant.sidebar.close"].exists)
            XCTAssertFalse(app.buttons["assistant.sidebar.settings"].exists)
            app.buttons["assistant.sidebar.options"].tap()
            capture("Sidebar settings and account menu — \(theme)")
            app.buttons["Settings"].tap()
            XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 5))
            app.buttons["assistant.settings.done"].tap()
            assertSidebarClosed(app)

            app.buttons["assistant.sidebar.open"].tap()
            app.buttons["assistant.sidebar.options"].tap()
            app.buttons["Account"].tap()
            XCTAssertTrue(app.navigationBars["Account"].waitForExistence(timeout: 5))
            app.buttons["assistant.settings.done"].tap()
            assertSidebarClosed(app)
            app.terminate()
        }
    }

    func testSidebarDeletionClearsTheOpenChatAndPreservesOtherDrafts() {
        for theme in ["light", "dark"] {
            let app = launch("assistant", theme: theme)
            let composer = app.descendants(matching: .any)["chat.composer"].firstMatch
            XCTAssertTrue(composer.waitForExistence(timeout: 15))
            composer.tap()
            composer.typeText("Keep this draft when another chat is deleted")
            let current = app.buttons["chat.history.saved.store-project-0-chat-0"]
            let other = app.buttons["chat.history.saved.store-project-0-chat-1"]
            let remaining = app.buttons["chat.history.saved.store-project-0-chat-2"]

            // Visit another conversation, then return to the original unfinished draft.
            app.buttons["assistant.sidebar.open"].tap()
            other.tap()
            app.buttons["assistant.sidebar.open"].tap()
            current.tap()
            XCTAssertEqual(composer.value as? String, "Keep this draft when another chat is deleted")
            app.buttons["assistant.sidebar.open"].tap()

            other.press(forDuration: 0.8)
            app.buttons["Delete"].firstMatch.tap()
            let confirmation = app.alerts["Delete chat?"]
            XCTAssertTrue(confirmation.waitForExistence(timeout: 5))
            confirmation.buttons["Cancel"].tap()
            XCTAssertTrue(other.isHittable)

            other.press(forDuration: 0.8)
            app.buttons["Delete"].firstMatch.tap()
            confirmation.buttons["Delete"].tap()
            XCTAssertTrue(other.waitForNonExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(app.buttons["assistant.sidebar.backdrop"].isHittable)
            app.buttons["assistant.sidebar.backdrop"].tap()
            XCTAssertEqual(composer.value as? String, "Keep this draft when another chat is deleted")

            app.buttons["assistant.sidebar.open"].tap()
            current.press(forDuration: 0.8)
            app.buttons["Delete"].firstMatch.tap()
            confirmation.buttons["Delete"].tap()
            XCTAssertTrue(current.waitForNonExistence(timeout: 5), app.debugDescription)
            XCTAssertTrue(app.buttons["assistant.sidebar.backdrop"].isHittable)
            app.buttons["assistant.sidebar.backdrop"].tap()
            XCTAssertTrue(app.buttons["Start chatting"].waitForExistence(timeout: 5), app.debugDescription)
            XCTAssertFalse(composer.exists, "A deleted chat must not leave its composer or loading thread mounted")
            capture("After deleting the open chat — \(theme)")

            app.buttons["assistant.sidebar.open"].tap()
            remaining.tap()
            XCTAssertTrue(composer.waitForExistence(timeout: 5))
            XCTAssertEqual(composer.value as? String, "")
            app.terminate()
        }
    }

    func testComputerChoicesKeepSavedComputersInTheirOwnBranch() {
        let app = launch("assistant-setup", theme: "light")
        let own = app.buttons["assistant.choose.computer"]
        let cloud = app.buttons["assistant.choose.cloud"]
        XCTAssertTrue(own.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertTrue(cloud.isHittable)
        capture("Choose your computer or a cloud computer — light")
        own.tap()
        XCTAssertTrue(app.navigationBars["My computer"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "MacBook Pro")).firstMatch.exists)
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "Cloud Studio")).firstMatch.exists)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(cloud.waitForExistence(timeout: 5))
        cloud.tap()
        XCTAssertTrue(app.navigationBars["Mindwire Cloud"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "MacBook Pro")).firstMatch.exists)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(own.waitForExistence(timeout: 5))
        app.terminate()
    }

    func testBotGroupsKeepChatsDraftsAndConversationalEditingSeparate() {
        for theme in ["light", "dark"] {
            let app = launch("assistant", theme: theme, content: "bots")
            let composer = app.descendants(matching: .any)["chat.composer"].firstMatch
            XCTAssertTrue(composer.waitForExistence(timeout: 15), app.debugDescription)
            app.buttons["assistant.sidebar.open"].tap()
            let search = app.textFields["assistant.sidebar.search"]
            search.tap()
            search.typeText("Writing Partner")
            let writingChat = app.buttons["chat.history.saved.store-writing-chat"]
            XCTAssertTrue(writingChat.waitForExistence(timeout: 5), app.debugDescription)
            writingChat.tap()
            XCTAssertTrue(app.staticTexts["Writing Partner"].waitForExistence(timeout: 5))
            composer.tap()
            composer.typeText("Keep my unfinished article")
            app.buttons["assistant.sidebar.open"].tap()
            app.buttons["assistant.sidebar.newChat"].tap()
            XCTAssertTrue(composer.waitForExistence(timeout: 5))
            XCTAssertEqual(composer.value as? String, "")
            XCTAssertTrue(app.staticTexts["Writing Partner"].exists)
            app.buttons["assistant.sidebar.open"].tap()
            if app.buttons["assistant.sidebar.clearSearch"].exists { app.buttons["assistant.sidebar.clearSearch"].tap() }
            let history = app.descendants(matching: .any)["chat.history.list"].firstMatch
            history.swipeDown()
            let master = app.staticTexts["assistant.bot.name.store-mindwire-store-project-0"]
            XCTAssertTrue(master.isHittable)
            master.tap()
            let writer = app.staticTexts["assistant.bot.name.store-bot-writing"]
            XCTAssertTrue(writer.isHittable, app.debugDescription)
            capture("General bots and grouped conversations — \(theme)")
            writer.tap()
            XCTAssertFalse(writingChat.isHittable)
            writer.tap()
            XCTAssertTrue(writingChat.isHittable)
            writingChat.tap()
            XCTAssertEqual(composer.value as? String, "Keep my unfinished article")

            app.buttons["assistant.sidebar.open"].tap()
            writer.press(forDuration: 0.8)
            app.buttons["About this bot"].firstMatch.tap()
            XCTAssertTrue(app.navigationBars["Writing Partner"].waitForExistence(timeout: 5))
            XCTAssertTrue(app.staticTexts["The user prefers short paragraphs."].exists)
            XCTAssertTrue(app.buttons["Style guide"].exists)
            capture("Saved bot instructions memory and knowledge — \(theme)")
            app.buttons["Edit with Mindwire"].tap()
            XCTAssertTrue(composer.waitForExistence(timeout: 5))
            XCTAssertTrue((composer.value as? String)?.contains("Help me improve my bot") == true)
            XCTAssertTrue(app.staticTexts["Mindwire"].exists)

            app.buttons["assistant.sidebar.open"].tap()
            app.buttons["assistant.sidebar.options"].tap()
            app.buttons["assistant.bots.create"].tap()
            XCTAssertTrue(composer.waitForExistence(timeout: 5))
            XCTAssertEqual(composer.value as? String, "Help me create a bot. ")
            app.terminate()
        }
    }

    func testArchivingAndRestoringABotPreservesItsChatAndDraft() {
        let app = launch("assistant", theme: "light", content: "bots")
        let composer = app.descendants(matching: .any)["chat.composer"].firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        app.buttons["assistant.sidebar.open"].tap()
        let search = app.textFields["assistant.sidebar.search"]
        search.tap()
        search.typeText("Writing Partner")
        let writingChat = app.buttons["chat.history.saved.store-writing-chat"]
        writingChat.tap()
        composer.tap()
        composer.typeText("Keep this draft through archive and restore")
        app.buttons["assistant.sidebar.open"].tap()
        let writer = app.staticTexts["assistant.bot.name.store-bot-writing"]
        writer.press(forDuration: 0.8)
        app.buttons["Archive bot"].firstMatch.tap()
        XCTAssertTrue(writer.waitForNonExistence(timeout: 5), app.debugDescription)
        app.buttons["assistant.sidebar.options"].tap()
        app.buttons["Archived bots"].tap()
        XCTAssertTrue(app.navigationBars["Archived bots"].waitForExistence(timeout: 5))
        app.buttons["Restore bot"].tap()
        app.buttons["Done"].tap()
        XCTAssertTrue(writer.waitForExistence(timeout: 5))
        writingChat.tap()
        XCTAssertEqual(composer.value as? String, "Keep this draft through archive and restore")
        app.terminate()
    }

    func testBotSidebarLimitsChatsAndOpensItsFullHistory() {
        for theme in ["light", "dark"] {
            let app = launch("assistant", theme: theme, content: "bots-history")
            let composer = app.descendants(matching: .any)["chat.composer"].firstMatch
            XCTAssertTrue(composer.waitForExistence(timeout: 15))
            composer.tap()
            composer.typeText("Keep the main assistant draft")
            app.buttons["assistant.sidebar.open"].tap()
            app.staticTexts["assistant.bot.name.store-mindwire-store-project-0"].tap()
            let older = app.buttons["chat.history.saved.store-writing-earlier-7"]
            let more = app.buttons["assistant.bot.more.store-bot-writing"]
            let rows = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "chat.history.saved."))
            XCTAssertEqual(rows.allElementsBoundByIndex.filter(\.isHittable).count, 5)
            XCTAssertFalse(older.exists, "Older chats belong in full history instead of extending the drawer")
            XCTAssertTrue(more.isHittable)
            capture("Five recent chats per bot — \(theme)")

            more.tap()
            XCTAssertTrue(app.navigationBars["Writing Partner"].waitForExistence(timeout: 5))
            let modalSearch = app.searchFields.firstMatch
            XCTAssertTrue(modalSearch.waitForExistence(timeout: 5))
            XCTAssertFalse(app.buttons["chat.history.saved.store-project-0-chat-0"].isHittable,
                           "A bot's modal must not include another bot's chats")
            capture("Complete bot history — \(theme)")
            let close = app.buttons["chat.history.close"]
            XCTAssertTrue(close.waitForExistence(timeout: 5))
            close.tap()
            XCTAssertTrue(more.waitForExistence(timeout: 5))
            app.buttons["assistant.sidebar.backdrop"].tap()
            XCTAssertEqual(composer.value as? String, "Keep the main assistant draft")

            app.buttons["assistant.sidebar.open"].tap()
            more.tap()
            XCTAssertTrue(modalSearch.waitForExistence(timeout: 5))
            XCTAssertNotEqual(modalSearch.value as? String, "Earlier article 8")
            modalSearch.tap()
            modalSearch.typeText("Earlier article 8")
            XCTAssertTrue(older.waitForExistence(timeout: 5))
            XCTAssertTrue(older.isHittable)
            capture("Full searchable bot history — \(theme)")
            older.tap()
            assertSidebarClosed(app)
            XCTAssertTrue(app.staticTexts["Writing Partner"].exists)
            XCTAssertTrue(composer.waitForExistence(timeout: 5))
            composer.tap()
            composer.typeText("Keep this older conversation draft")

            app.buttons["assistant.sidebar.open"].tap()
            let sidebarSearch = app.textFields["assistant.sidebar.search"]
            sidebarSearch.tap()
            sidebarSearch.typeText("Earlier article 8")
            XCTAssertTrue(older.waitForExistence(timeout: 5), "Sidebar search must find chats outside the recent five")
            XCTAssertFalse(more.exists)
            older.tap()
            XCTAssertEqual(composer.value as? String, "Keep this older conversation draft")
            app.terminate()
        }
    }

    func testBotIconsCanBeChangedFromSettingsAndKeepTheirChatColor() {
        for theme in ["light", "dark"] {
            let app = launch("assistant", theme: theme, content: "bots")
            let composer = app.descendants(matching: .any)["chat.composer"].firstMatch
            XCTAssertTrue(composer.waitForExistence(timeout: 15))
            app.buttons["assistant.sidebar.open"].tap()
            app.buttons["assistant.sidebar.options"].tap()
            app.buttons["Settings"].tap()
            let icons = app.buttons["settings.botIcons"]
            for _ in 0..<3 where !icons.isHittable { app.swipeUp() }
            XCTAssertTrue(icons.waitForExistence(timeout: 5), app.debugDescription)
            icons.tap()
            app.buttons["assistant.icon.options.store-bot-writing"].tap()
            app.buttons["Change icon"].tap()
            let blue = app.buttons["assistant.icon.color.blue"]
            XCTAssertTrue(blue.waitForExistence(timeout: 5))
            blue.tap()
            app.buttons["assistant.icon.character.spark"].tap()
            capture("Bot character and color picker — \(theme)")
            app.buttons["assistant.icon.save"].tap()
            XCTAssertTrue(app.navigationBars["Bot icons"].waitForExistence(timeout: 5))
            app.buttons["assistant.icon.options.store-bot-writing"].tap()
            app.buttons["Change icon"].tap()
            XCTAssertTrue(blue.waitForExistence(timeout: 5))
            XCTAssertTrue(blue.isSelected)
            XCTAssertTrue(app.buttons["assistant.icon.character.spark"].isSelected)
            app.buttons["Cancel"].tap()
            app.navigationBars.buttons.element(boundBy: 0).tap()
            app.buttons["assistant.settings.done"].tap()
            app.buttons["assistant.sidebar.open"].tap()
            let search = app.textFields["assistant.sidebar.search"]
            search.tap(); search.typeText("Writing Partner")
            let chat = app.buttons["chat.history.saved.store-writing-chat"]
            XCTAssertTrue(chat.waitForExistence(timeout: 5))
            capture("Custom blue bot icon in sidebar — \(theme)")
            chat.tap()
            XCTAssertTrue(composer.waitForExistence(timeout: 5))
            app.tables["chat.transcript"].swipeDown()
            app.tables["chat.transcript"].swipeDown()
            capture("Outgoing messages use the bot color — \(theme)")
            app.terminate()
        }
    }

    func testComputerChoicesRemainReachableWithAccessibilityText() {
        let app = launch("assistant-setup", theme: "dark", largeText: true)
        XCTAssertTrue(app.buttons["assistant.choose.computer"].waitForExistence(timeout: 15), app.debugDescription)
        let cloud = app.buttons["assistant.choose.cloud"]
        for _ in 0..<5 where !cloud.isHittable { app.swipeUp() }
        XCTAssertTrue(cloud.isHittable)
        capture("Computer choices — dark with accessibility text")
        cloud.tap()
        XCTAssertTrue(app.navigationBars["Mindwire Cloud"].waitForExistence(timeout: 5))
        app.terminate()
    }

    private func assertSidebarClosed(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        // The drawer stays mounted for stable layout, so check visibility and interaction,
        // rather than requiring UIKit's offscreen list elements to leave the hierarchy.
        XCTAssertTrue(app.buttons["assistant.sidebar.backdrop"].waitForNonExistence(timeout: 5), file: file, line: line)
        XCTAssertTrue(app.buttons["assistant.sidebar.open"].isHittable, file: file, line: line)
        XCTAssertFalse(app.textFields["assistant.sidebar.search"].isHittable, file: file, line: line)
    }

    private func scrollOnboarding(_ app: XCUIApplication, above footer: XCUIElement) {
        // Scroll inside the visible content rather than swiping across its fixed action.
        let origin = app.coordinate(withNormalizedOffset: .zero)
        let start = origin.withOffset(CGVector(dx: app.frame.midX, dy: footer.frame.minY - 40))
        let end = origin.withOffset(CGVector(dx: app.frame.midX, dy: 120))
        start.press(forDuration: 0.05, thenDragTo: end)
    }

    private func launch(_ scene: String, theme: String, largeText: Bool = false, content: String = "controls") -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["--app-store-preview", "-AppleLanguages", "(en)", "-AppleLocale", "en_US"]
        if largeText { app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"] }
        app.launchEnvironment = ["MINDWIRE_STORE_SCENE": scene, "MINDWIRE_STORE_THEME": theme,
                                 "MINDWIRE_CHAT_TEST_CONTENT": content]
        app.launch()
        return app
    }

    private func capture(_ name: String) {
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
#endif
