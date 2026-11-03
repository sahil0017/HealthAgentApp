import SwiftUI
import AuthenticationServices
import CryptoKit

// Sign-in mirrors the web app: the API's /auth/config says which providers are enabled, the user signs in with
// Google or Apple, and the verified ID token is exchanged at /auth/session. The session stays in the Keychain.

struct AuthProviderInfo: Decodable, Identifiable, Sendable {
    let name: String
    let clientId: String
    let iosClientId: String?
    var id: String { name }
}

private struct AuthConfigResponse: Decodable { let providers: [AuthProviderInfo] }

enum AuthError: LocalizedError {
    case missingToken
    case cancelled
    case failed(String)
    var errorDescription: String? {
        switch self {
        case .missingToken: return "The provider did not return an identity token."
        case .cancelled: return nil
        case .failed(let message): return message
        }
    }
}

enum AuthHelpers {
    static func randomNonce() -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    static func sha256Hex(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    static func base64URL(_ data: Data) -> String {
        data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
    }
}

/// Google sign-in for a native app: authorization code with PKCE in a system web session, then the code is
/// exchanged for an ID token. No client secret is involved.
@MainActor
final class GoogleSignIn: NSObject, ASWebAuthenticationPresentationContextProviding {
    func idToken(clientId: String, nonce: String) async throws -> String {
        let verifier = AuthHelpers.base64URL(Data((0..<32).map { _ in UInt8.random(in: 0...255) }))
        let challenge = AuthHelpers.base64URL(Data(SHA256.hash(data: Data(verifier.utf8))))
        let scheme = "com.googleusercontent.apps." + clientId.replacingOccurrences(of: ".apps.googleusercontent.com", with: "")
        let redirect = scheme + ":/oauth2redirect"

        var components = URLComponents(string: "https://accounts.google.com/o/oauth2/v2/auth")!
        components.queryItems = [
            .init(name: "client_id", value: clientId),
            .init(name: "redirect_uri", value: redirect),
            .init(name: "response_type", value: "code"),
            .init(name: "scope", value: "openid"),
            .init(name: "nonce", value: nonce),
            .init(name: "code_challenge", value: challenge),
            .init(name: "code_challenge_method", value: "S256"),
        ]
        let callback: URL = try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(url: components.url!, callbackURLScheme: scheme) { url, error in
                if let error = error as? ASWebAuthenticationSessionError, error.code == .canceledLogin {
                    continuation.resume(throwing: AuthError.cancelled)
                } else if let error {
                    continuation.resume(throwing: error)
                } else if let url {
                    continuation.resume(returning: url)
                } else {
                    continuation.resume(throwing: AuthError.missingToken)
                }
            }
            session.presentationContextProvider = self
            session.prefersEphemeralWebBrowserSession = false
            if !session.start() { continuation.resume(throwing: AuthError.failed("Could not open the Google sign-in page.")) }
        }
        guard let code = URLComponents(url: callback, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "code" })?.value else {
            throw AuthError.missingToken
        }

        var request = URLRequest(url: URL(string: "https://oauth2.googleapis.com/token")!)
        request.httpMethod = "POST"
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        var form = URLComponents()
        form.queryItems = [
            .init(name: "client_id", value: clientId),
            .init(name: "code", value: code),
            .init(name: "code_verifier", value: verifier),
            .init(name: "grant_type", value: "authorization_code"),
            .init(name: "redirect_uri", value: redirect),
        ]
        request.httpBody = form.percentEncodedQuery?.data(using: .utf8)
        let (data, response) = try await URLSession.shared.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200,
              let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let token = object["id_token"] as? String else { throw AuthError.missingToken }
        return token
    }

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow }.first ?? ASPresentationAnchor()
    }
}

struct SignInSection: View {
    @EnvironmentObject private var model: CortexModel
    @State private var providers: [AuthProviderInfo]?
    @State private var appleNonce = ""
    @State private var busy = false
    @State private var google = GoogleSignIn()

    var body: some View {
        Section("Sign in") {
            if let providers {
                if providers.isEmpty {
                    Text("Sign-in is not set up on this server yet.").font(.footnote).foregroundStyle(.secondary)
                }
                ForEach(providers) { provider in
                    if provider.name == "apple" {
                        SignInWithAppleButton(.continue) { request in
                            appleNonce = AuthHelpers.randomNonce()
                            request.requestedScopes = []
                            request.nonce = AuthHelpers.sha256Hex(appleNonce)
                        } onCompletion: { result in
                            handleApple(result)
                        }
                        .frame(height: 44)
                        .disabled(busy)
                    } else if provider.name == "google" {
                        Button { Task { await signInWithGoogle(provider) } } label: {
                            Label("Continue with Google", systemImage: "g.circle").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                        .disabled(busy || (provider.iosClientId ?? "").isEmpty)
                    }
                }
            } else {
                ProgressView()
            }
        }
        .task { await loadProviders() }
        .onChange(of: model.apiURL) { _ in Task { await loadProviders() } }
    }

    private func loadProviders() async {
        providers = nil
        do {
            providers = try await model.authProviders()
        } catch {
            providers = []
            model.message = error.localizedDescription
        }
    }

    private func handleApple(_ result: Result<ASAuthorization, Error>) {
        switch result {
        case .success(let auth):
            guard let credential = auth.credential as? ASAuthorizationAppleIDCredential,
                  let data = credential.identityToken, let idToken = String(data: data, encoding: .utf8) else {
                model.message = AuthError.missingToken.localizedDescription
                return
            }
            let nonce = appleNonce
            Task { busy = true; await model.connect(provider: "apple", idToken: idToken, nonce: nonce); busy = false }
        case .failure(let error):
            if (error as? ASAuthorizationError)?.code != .canceled { model.message = error.localizedDescription }
        }
    }

    private func signInWithGoogle(_ provider: AuthProviderInfo) async {
        guard let clientId = provider.iosClientId, !clientId.isEmpty else { return }
        busy = true
        defer { busy = false }
        let nonce = AuthHelpers.randomNonce()
        do {
            let idToken = try await google.idToken(clientId: clientId, nonce: nonce)
            await model.connect(provider: "google", idToken: idToken, nonce: nonce)
        } catch AuthError.cancelled {
        } catch {
            model.message = error.localizedDescription
        }
    }
}
