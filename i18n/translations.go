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
	PostsTitle       string
	SearchPlaceholder string
	SearchButton     string
	NoPostsFound     string

	// Footer
	FooterCopyright string
	FooterVisits    string

	// Home page
	HeroTitle    string
	HeroRole     string
	HeroTagline  string
	HeroBio      string

	// Tech categories
	TechBackendTitle     string
	TechBackendDesc      string
	TechDistributedTitle string
	TechDistributedDesc  string
	TechInfraTitle       string
	TechInfraDesc        string

	// About page
	AboutTitle   string
	AboutIntro   string
	AboutBlog    string
	AboutContact string
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

		// Footer
		FooterCopyright: "Paulo Vicente. All rights reserved.",
		FooterVisits:    "visits",

		// Home page
		HeroTitle:   "Paulo Vicente",
		HeroRole:    "Backend Software Engineer",
		HeroTagline: "Building scalable, resilient systems with Golang and modern distributed architectures",
		HeroBio:     "I'm a Backend Software Engineer with a passion for building scalable, resilient systems using Golang and modern distributed architectures. With years of experience designing high-performance backends—leveraging Go's concurrency model, efficient memory management, and compile-time safety—I specialize in turning challenging business requirements into efficient, maintainable solutions.",

		// Tech categories
		TechBackendTitle:     "Backend & Architecture",
		TechBackendDesc:      "Expert in Golang, microservices, RESTful APIs, gRPC, and clean architecture patterns. Experienced with Domain-Driven Design (DDD) and building maintainable, testable systems",
		TechDistributedTitle: "Distributed Systems",
		TechDistributedDesc:  "Deep experience with event-driven architectures, CQRS, Event Sourcing, and message brokers (Kafka, RabbitMQ). Focus on consistency patterns, fault tolerance, and systems that scale",
		TechInfraTitle:       "Infrastructure & DevOps",
		TechInfraDesc:        "Hands-on with Docker, Kubernetes, AWS, CI/CD pipelines, and observability tools",

		// About page
		AboutTitle:       "About Me",
		AboutIntro:       "Hi! I'm Paulo Vicente, a software developer passionate about programming and technology.",
		AboutBlog:        "This blog is where I share my thoughts, experiences, and learnings about software development, programming languages, and various technical topics that interest me.",
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

		// Footer
		FooterCopyright: "Paulo Vicente. Todos os direitos reservados.",
		FooterVisits:    "visitas",

		// Home page
		HeroTitle:   "Paulo Vicente",
		HeroRole:    "Engenheiro de Software Backend",
		HeroTagline: "Construindo sistemas escaláveis e resilientes com Golang e arquiteturas distribuídas modernas",
		HeroBio:     "Sou um Engenheiro de Software Backend com paixão por construir sistemas escaláveis e resilientes usando Golang e arquiteturas distribuídas modernas. Com anos de experiência projetando backends de alto desempenho—aproveitando o modelo de concorrência do Go, gerenciamento eficiente de memória e segurança em tempo de compilação—me especializo em transformar requisitos de negócio desafiadores em soluções eficientes e mantíveis.",

		// Tech categories
		TechBackendTitle:     "Backend & Arquitetura",
		TechBackendDesc:      "Especialista em Golang, microsserviços, APIs RESTful, gRPC e padrões de arquitetura limpa. Experiente com Domain-Driven Design (DDD) e construção de sistemas testáveis e mantíveis",
		TechDistributedTitle: "Sistemas Distribuídos",
		TechDistributedDesc:  "Experiência profunda com arquiteturas orientadas a eventos, CQRS, Event Sourcing e message brokers (Kafka, RabbitMQ). Foco em padrões de consistência, tolerância a falhas e sistemas que escalam",
		TechInfraTitle:       "Infraestrutura & DevOps",
		TechInfraDesc:        "Experiência prática com Docker, Kubernetes, AWS, pipelines CI/CD e ferramentas de observabilidade",

		// About page
		AboutTitle:       "Sobre Mim",
		AboutIntro:       "Olá! Sou Paulo Vicente, um desenvolvedor de software apaixonado por programação e tecnologia.",
		AboutBlog:        "Este blog é onde compartilho meus pensamentos, experiências e aprendizados sobre desenvolvimento de software, linguagens de programação e vários tópicos técnicos que me interessam.",
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
