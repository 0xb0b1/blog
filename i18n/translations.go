package i18n

// Lang represents a supported language
type Lang string

const (
	EN Lang = "en"
	PT Lang = "pt"
)

// Translations holds all translated strings for a language
type Translations struct {
	// Navigation
	NavHome  string
	NavPosts string
	NavAbout string

	// Posts page
	PostsTitle        string
	SearchPlaceholder string
	SearchButton      string
	NoPostsFound      string
	AllTags           string
	FilterByTag       string
	PrevPage          string
	NextPage          string
	PageOf            string

	// Footer
	FooterCopyright string

	// Home page
	HeroTitle   string
	HeroRole    string
	HeroTagline string
	HeroBio     string

	// Tech categories
	TechBackendTitle     string
	TechBackendDesc      string
	TechDistributedTitle string
	TechDistributedDesc  string
	TechInfraTitle       string
	TechInfraDesc        string
	TechFunctionalTitle  string
	TechFunctionalDesc   string

	// About page
	AboutTitle       string
	AboutIntro       string
	AboutBlog        string
	AboutContact     string
	AboutContactLink string
}

var translations = map[Lang]Translations{
	EN: {
		// Navigation
		NavHome:  "Home",
		NavPosts: "Posts",
		NavAbout: "About",

		// Posts page
		PostsTitle:        "Posts",
		SearchPlaceholder: "Search posts...",
		SearchButton:      "Search",
		NoPostsFound:      "No posts found for",
		AllTags:           "All",
		FilterByTag:       "Filter by tag",
		PrevPage:          "Previous",
		NextPage:          "Next",
		PageOf:            "of",

		// Footer
		FooterCopyright: "Paulo Vicente. All rights reserved.",

		// Home page
		HeroTitle:   "Paulo Vicente",
		HeroRole:    "Backend Software Engineer & Cybersecurity Enthusiast",
		HeroTagline: "Building resilient, data-heavy systems with Clojure and Go — modern distributed architectures and a growing interest in application security",
		HeroBio:     "I'm a Backend Software Engineer with years of experience building production systems in Clojure and Go. In Clojure I've built data-heavy applications — service-oriented backends, typed HTTP contracts and backend-for-frontends with Ring and Reitit, reactive front-ends with re-frame, and immutable, time-aware data models on Datomic. In Go I build microservices and event-driven backends handling millions of requests. I turn challenging business requirements into maintainable, well-architected solutions, and lately I've been studying application security — understanding how systems break is making me a better engineer.",

		// Tech categories
		TechBackendTitle:     "Backend & Architecture",
		TechBackendDesc:      "Golang, microservices, RESTful APIs, gRPC, and clean architecture patterns. Experienced with Domain-Driven Design (DDD) and building maintainable, testable systems",
		TechDistributedTitle: "Distributed Systems",
		TechDistributedDesc:  "Event-driven architectures, CQRS, Event Sourcing, and message brokers (Kafka, RabbitMQ). Focus on consistency patterns, fault tolerance, and resilient design",
		TechInfraTitle:       "Infrastructure & Security",
		TechInfraDesc:        "Docker, Kubernetes, AWS, CI/CD pipelines, and observability. Currently studying application security and secure development practices",
		TechFunctionalTitle:  "Functional Programming",
		TechFunctionalDesc:   "Clojure and ClojureScript for data-heavy products: typed HTTP contracts with Ring, Reitit and malli, backend-for-frontend patterns, reactive UIs with re-frame, concurrent data pipelines with core.async, and Datomic for immutable data with built-in history.",

		// About page
		AboutTitle:       "About Me",
		AboutIntro:       "Hi! I'm Paulo Vicente, a backend software engineer who builds systems in Clojure and Go — from data-heavy Clojure applications to distributed Go backends — and writes about the real-world tradeoffs behind them. Currently studying application security.",
		AboutBlog:        "This blog is where I share what I learn about software architecture, functional programming in Clojure, distributed systems, and the security lessons I pick up along the way.",
		AboutContact:     "Get in Touch",
		AboutContactLink: "Feel free to reach out to me on",
	},
	PT: {
		// Navigation
		NavHome:  "Início",
		NavPosts: "Posts",
		NavAbout: "Sobre",

		// Posts page
		PostsTitle:        "Posts",
		SearchPlaceholder: "Pesquisar posts...",
		SearchButton:      "Pesquisar",
		NoPostsFound:      "Nenhum post encontrado para",
		AllTags:           "Todos",
		FilterByTag:       "Filtrar por tag",
		PrevPage:          "Anterior",
		NextPage:          "Próximo",
		PageOf:            "de",

		// Footer
		FooterCopyright: "Paulo Vicente. Todos os direitos reservados.",

		// Home page
		HeroTitle:   "Paulo Vicente",
		HeroRole:    "Engenheiro de Software Backend & Entusiasta de Cybersecurity",
		HeroTagline: "Construindo sistemas resilientes e com muitos dados usando Clojure e Go — arquiteturas distribuídas modernas e um interesse crescente em application security",
		HeroBio:     "Sou Engenheiro de Software Backend com anos de experiência construindo sistemas em produção com Clojure e Go. Em Clojure construí aplicações com muitos dados — backends orientados a serviços, contratos HTTP tipados e backend-for-frontends com Ring e Reitit, front-ends reativos com re-frame, e modelos de dados imutáveis e temporais no Datomic. Em Go construo microsserviços e backends orientados a eventos processando milhões de requisições. Transformo requisitos de negócio desafiadores em soluções mantíveis e bem arquitetadas, e ultimamente tenho estudado application security — entender como sistemas quebram está me tornando um engenheiro melhor.",

		// Tech categories
		TechBackendTitle:     "Backend & Arquitetura",
		TechBackendDesc:      "Golang, microsserviços, APIs RESTful, gRPC e padrões de arquitetura limpa. Experiente com Domain-Driven Design (DDD) e construção de sistemas testáveis e mantíveis",
		TechDistributedTitle: "Sistemas Distribuídos",
		TechDistributedDesc:  "Arquiteturas orientadas a eventos, CQRS, Event Sourcing e message brokers (Kafka, RabbitMQ). Foco em padrões de consistência, tolerância a falhas e design resiliente",
		TechInfraTitle:       "Infraestrutura & Segurança",
		TechInfraDesc:        "Docker, Kubernetes, AWS, pipelines CI/CD e observabilidade. Atualmente estudando application security e práticas de desenvolvimento seguro",
		TechFunctionalTitle:  "Programação Funcional",
		TechFunctionalDesc:   "Clojure e ClojureScript para produtos com muitos dados: contratos HTTP tipados com Ring, Reitit e malli, padrões backend-for-frontend, UIs reativas com re-frame, pipelines de dados concorrentes com core.async, e Datomic para dados imutáveis com histórico embutido.",

		// About page
		AboutTitle:       "Sobre Mim",
		AboutIntro:       "Olá! Sou Paulo Vicente, engenheiro de software backend que constrói sistemas em Clojure e Go — de aplicações Clojure com muitos dados a backends distribuídos em Go — e escreve sobre os tradeoffs reais por trás deles. Atualmente estudando application security.",
		AboutBlog:        "Este blog é onde compartilho o que aprendo sobre arquitetura de software, programação funcional em Clojure, sistemas distribuídos e as lições de segurança que vou aprendendo no caminho.",
		AboutContact:     "Entre em Contato",
		AboutContactLink: "Fique à vontade para me contatar no",
	},
}

// Get returns the translations for the given language
func Get(lang Lang) Translations {
	if t, ok := translations[lang]; ok {
		return t
	}
	return translations[EN] // Default to English
}

// GetLang parses a language string and returns the corresponding Lang
func GetLang(s string) Lang {
	switch s {
	case "pt":
		return PT
	default:
		return EN
	}
}

// SupportedLanguages returns all supported languages
func SupportedLanguages() []Lang {
	return []Lang{EN, PT}
}

// OtherLang returns the other language (for language switcher)
func OtherLang(lang Lang) Lang {
	if lang == EN {
		return PT
	}
	return EN
}

// LangName returns the display name for a language
func LangName(lang Lang) string {
	switch lang {
	case PT:
		return "Português"
	default:
		return "English"
	}
}

// LangCode returns the short code for a language
func LangCode(lang Lang) string {
	return string(lang)
}
