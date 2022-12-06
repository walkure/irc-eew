package EEWSock;

# EEWSock.pm
# walkure at 3pf.jp

use strict;
use warnings;
use base qw/IO::Socket::INET/;

use Encode;
use Time::Piece;
use Time::HiRes;
use HTTP::Tiny;
use HTTP::Headers;
use Digest::MD5 qw(md5_hex);
use Socket qw(SOL_SOCKET SO_KEEPALIVE IPPROTO_TCP TCP_KEEPINTVL TCP_KEEPIDLE TCP_KEEPCNT);

sub configure
{
	my ( $self, $args ) = @_;
	
	*$self->{server_list} = $args->{ServerList} if defined $args->{ServerList};
	*$self->{callback} = $args->{Callback} if defined $args->{Callback};
	
	*$self->{header} = {};
	*$self->{buffer} = '';
	
	$self->get_server_list() unless defined *$self->{server_list};
	
	my ($host,$port) = $self->choose_server();
	print "+Choose $host:$port\n";
	
	$args->{PeerAddr} = "$host:$port";

	*$self->{timeout_log} = $args->{Logs}{Timeout};
	*$self->{keepalive_log} = $args->{Logs}{KeepAlive};
	
	print "+Timeout Log Enabled\n" if *$self->{timeout_log};
	print "+KeepAlive Log Enabled\n" if *$self->{keepalive_log};
	
	my $sock = $self->SUPER::configure($args);

	# activate TCP Keep-Alive
	$sock->setsockopt(SOL_SOCKET, SO_KEEPALIVE,  1);
	# keep-alive packet interval 15secs.
	$sock->setsockopt(IPPROTO_TCP, TCP_KEEPINTVL, 15);
	# idle wait 4minutes
	$sock->setsockopt(IPPROTO_TCP, TCP_KEEPIDLE,  60 * 3 );

	$sock;
}

sub choose_server
{
	my $self = shift;
	my @server = @{*$self->{server_list}};
	my $sum = scalar @server;
	my ( $ip, $port ) = split /:/, $server[ int( rand($sum) ) ];
	
	($ip,$port);
}

sub get_server_list
{
	my $self = shift;
	
	if(ref($self) eq 'EEWSock' && defined *$self->{server_list}){
		print 'Cached '.scalar @{*$self->{server_list}}." servers\n";
		return *$self->{server_list};
	}
	
	my $ua = HTTP::Tiny->new(agent => 'FastCaster/1.0 powered by weathernews.');
	my @server;
	
	my $res = $ua->get('http://lst10s-sp.wni.co.jp/server_list.txt');
	if ( $res->{success}) {
		@server = split /[\r\n]+/, $res->{content};
	}
	else {
	    die "Cannot get server list: $res->{status} $res->{reason} \n";
	}
	print 'Retrieved '.scalar @server." servers\n";
	
	if(ref($self) eq 'EEWSock'){
		*$self->{server_list} = \@server;
	}else{
		\@server;
	}
}

sub wni_timestamp
{
	my ($seconds,$microseconds) = Time::HiRes::gettimeofday;
	sprintf '%s.%06d', localtime($seconds)->strftime('%Y/%m/%d %H:%M:%S'), $microseconds;
}

sub send_ack
{
	my $self = shift;
	
	my $now = wni_timestamp();
	my $response = HTTP::Headers->new(
		'Content-Type' => 'application/fast-cast',
		'Server' => 'FastCaster/1.0.0 (Unix)',
		'X-WNI-ID' => 'Response',
		'X-WNI-Result' => 'OK',
		'X-WNI-Protocol-Version' => '2.1',
		'X-WNI-Time' => $now
	);

	my $msg = "HTTP/1.0 200 OK\n" . $response->as_string ."\n";
	$self->syswrite($msg);

	print scalar localtime." Send Request(Timeout) Waiting...\n" if *$self->{timeout_log};
}

sub logon
{
	my ($self,$conf) = @_;
	
	my $hash = defined $conf->{'passwd-md5'} ? $conf->{'passwd-md5'} : md5_hex($conf->{passwd});

	my $now = wni_timestamp();
	my $request = HTTP::Headers->new(
		'User-Agent' => 'FastCaster/1.0 powered by weathernews.',
		'Accept' => '*/*',
		'Cache-Control' => 'no-cache',
		'X-WNI-Account' => $conf->{user},
		'X-WNI-Password' => $hash,
		'X-WNI-Application-Version' => '2.2.4.0',
		'X-WNI-Authentication-Method' => 'MDB_MWS',
		'X-WNI-ID' => 'Login',
		'X-WNI-Protocol-Version' => '2.1',
		'X-WNI-Terminal-ID' => '211363088',
		'X-WNI-Time' => $now
	);
	
	my $body = "GET /login HTTP/1.0\n" . $request->as_string .  "\n" ;
	$self->syswrite($body);

	print scalar localtime." Send Request waiting...\n";
}

sub parse_body
{
	my ($self,$body) = @_;
	
	*$self->{buffer} .= $body;
	
	unless(defined *$self->{size}){
		my $i;
		while (($i = index(*$self->{buffer},"\x0a")) >= 0){ #index for the head of string is 0
			my $line = substr(*$self->{buffer},0,$i);
			*$self->{buffer} = substr(*$self->{buffer},$i+1);
			if($line =~ /GET \/ HTTP\/1.1/){
				$self->send_ack();
				*$self->{header} = {};
				*$self->{buffer} = '';
				next;
			}
			if($line =~ /:/){
				my($name,$body) = split(/:\s*/,$line);
				*$self->{header}{$name} = $body;
				next;
			}
			
			unless(length $line){
				my $id = *$self->{header}{'X-WNI-ID'};
				if($id eq 'Data'){
					if(defined *$self->{header}{'Content-Length'}){
						print 'Begin Contents('.*$self->{header}{'Content-Length'}.")\n";
						*$self->{size} = *$self->{header}{'Content-Length'} + 0;
						#break from while
						last;
					}
				}elsif($id eq 'Response'){
					print 'Auth Status:'.*$self->{header}{'X-WNI-Result'}."\n";
				}elsif($id eq 'Keep-Alive'){
					print scalar localtime." Keep-Alive...\n"  if *$self->{keepalive_log};
				}else{
					print "STATE:[$id]\n";
				}
				*$self->{size} = undef;
			}
		}
	}
	
	if(defined *$self->{size}){
		if(*$self->{size} == length *$self->{buffer}){
			if(defined *$self->{callback}){
				*$self->{callback}->($self,*$self->{header}{'X-WNI-Data-MD5'},*$self->{buffer});
			}
			*$self->{size} = undef;
			*$self->{buffer} = '';
		}
	}
}

sub set_callback
{
	my($self,$func) = @_;
	*$self->{callback} = $func;
}


1;

